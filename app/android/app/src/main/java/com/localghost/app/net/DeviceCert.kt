package com.localghost.app.net

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.security.keystore.KeyProtection
import android.util.Base64
import com.localghost.app.security.BoxConfig
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import java.io.ByteArrayInputStream
import java.net.Socket
import java.security.KeyFactory
import java.security.Principal
import java.security.PrivateKey
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.security.spec.PKCS8EncodedKeySpec
import javax.net.ssl.X509KeyManager

/**
 * The device identity: a client certificate and the private key the phone presents in the mutual
 * TLS handshake with the box.
 *
 * WHERE THE KEY LIVES. The enrolment QR carries a certificate AND its private key (the box made
 * both, so enrolment is one scan). Since 30 Sep 2026 that key goes straight into the phone's
 * secure element (AndroidKeyStore, imported as non-exportable): no copy of it remains in the app's
 * files, and code running as the app can use it but cannot read it out. A phone enrolled before
 * moves its key there the first time it connects.
 *
 * THEN IT IS REPLACED. The QR's key existed off the phone (on the box while the QR was drawn, in
 * any photo of the QR). After the first PIN unlock the phone makes a key of its own INSIDE the
 * Keystore, proves it holds it, and the box signs a certificate for it (secd, rekey.go). The phone
 * switches over and confirms over the new certificate; the box then retires the QR's: anything that
 * presents it is answered as if the box were down, the PIN entry included. Until the confirmation
 * both work, so a lost answer costs nothing: the next unlock finishes the switch.
 */
object DeviceCert {

    private const val ALIAS = "device-cert" // the certificate's name in BoxConfig's store (and the QR key's, before the Keystore)
    private const val PREFS = "lg_device_key"
    private const val KS_PREFIX = "localghost.device."
    private const val REKEY_MESSAGE = "localghost rekey v1\n"

    /** Parsed device identity. */
    data class Identity(val certificate: X509Certificate, val privateKey: PrivateKey)

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private fun ks(): KeyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

    /** The Keystore alias presented now; "" while the key is still the app's wrapped copy. */
    fun activeAlias(ctx: Context): String = prefs(ctx).getString("alias", "") ?: ""

    /** True once the phone presents a key it made itself (the QR's is retired on the box). */
    fun rotated(ctx: Context): Boolean = prefs(ctx).getBoolean("rotated", false)

    /** A new enrolment (a scanned QR): whatever this phone presented before goes; the QR's key goes
     *  into the Keystore at once. */
    fun store(ctx: Context, certPem: String, keyPkcs8Pem: String) {
        dropKeystoreKeys(except = "")
        prefs(ctx).edit().clear().commit()
        BoxConfig.writeSecret(ctx, "$ALIAS.cert", certPem, now = true)
        BoxConfig.removeSecret(ctx, "$ALIAS.cert.next")
        if (!importKey(ctx, certPem, keyPkcs8Pem, KS_PREFIX + "1")) {
            // the Keystore would not take it: kept wrapped in the app's store, as before this build
            BoxConfig.writeSecret(ctx, "$ALIAS.key", keyPkcs8Pem, now = true)
        }
    }

    /** Un-enrolment: the keys leave the Keystore too. */
    fun forget(ctx: Context) {
        dropKeystoreKeys(except = "")
        prefs(ctx).edit().clear().commit()
    }

    fun isEnrolled(ctx: Context): Boolean =
        BoxConfig.readSecret(ctx, "$ALIAS.cert") != null &&
            (activeAlias(ctx).isNotEmpty() || BoxConfig.readSecret(ctx, "$ALIAS.key") != null)

    fun load(ctx: Context): Identity? {
        val certPem = BoxConfig.readSecret(ctx, "$ALIAS.cert") ?: return null
        val cert = try { parseCert(certPem) } catch (e: Exception) { return null }
        val alias = activeAlias(ctx)
        if (alias.isNotEmpty()) {
            val key = try { ks().getKey(alias, null) as? PrivateKey } catch (e: Exception) { null }
            if (key != null) return Identity(cert, key)
        }
        // enrolled before 30 Sep 2026: the QR's key, wrapped in the app's store. Into the Keystore now.
        val keyPem = BoxConfig.readSecret(ctx, "$ALIAS.key") ?: return null
        val soft = try { parseKey(keyPem) } catch (e: Exception) { return null }
        if (importKey(ctx, certPem, keyPem, KS_PREFIX + "1")) {
            val key = try { ks().getKey(KS_PREFIX + "1", null) as? PrivateKey } catch (e: Exception) { null }
            if (key != null) return Identity(cert, key)
        }
        return Identity(cert, soft)
    }

    /** The QR's key into the Keystore, non-exportable; the app's wrapped copy removed. */
    private fun importKey(ctx: Context, certPem: String, keyPem: String, alias: String): Boolean = try {
        val cert = parseCert(certPem)
        val key = parseKey(keyPem)
        val prot = KeyProtection.Builder(KeyProperties.PURPOSE_SIGN)
            .setDigests(KeyProperties.DIGEST_NONE, KeyProperties.DIGEST_SHA256, KeyProperties.DIGEST_SHA384, KeyProperties.DIGEST_SHA512)
        if (key.algorithm == "RSA") prot.setSignaturePaddings(KeyProperties.SIGNATURE_PADDING_RSA_PKCS1, KeyProperties.SIGNATURE_PADDING_RSA_PSS)
        ks().setEntry(alias, KeyStore.PrivateKeyEntry(key, arrayOf<java.security.cert.Certificate>(cert)), prot.build())
        prefs(ctx).edit().putString("alias", alias).commit()
        BoxConfig.removeSecret(ctx, "$ALIAS.key")
        BoxHttp.reset()
        true
    } catch (e: Exception) {
        android.util.Log.w("LocalGhost", "device key not moved into the Keystore: ${e.javaClass.simpleName} ${e.message}")
        false
    }

    /**
     * After a PIN unlock, until it has happened once: the phone's own key replaces the QR's (see the
     * top of this file). True when the phone presents its own key and the box has retired the QR's.
     * Blocking network and Keystore work: call it off the main thread.
     */
    suspend fun rotateIfNeeded(ctx: Context): Boolean {
        val p = prefs(ctx)
        if (p.getBoolean("rotated", false)) return true
        finishSwitch(ctx) // a switch the app died in the middle of
        if (p.getBoolean("confirm_pending", false)) return confirm(ctx)
        if (activeAlias(ctx).isEmpty() && load(ctx) != null && activeAlias(ctx).isEmpty()) return false // not in the Keystore: not rotated from here
        val cur = activeAlias(ctx)
        if (cur.isEmpty()) return false
        val alias = KS_PREFIX + ((cur.removePrefix(KS_PREFIX).toIntOrNull() ?: 1) + 1)
        return try {
            try { ks().deleteEntry(alias) } catch (_: Exception) { }
            val kpg = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore")
            kpg.initialize(KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_SIGN)
                .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
                .setDigests(KeyProperties.DIGEST_NONE, KeyProperties.DIGEST_SHA256, KeyProperties.DIGEST_SHA384, KeyProperties.DIGEST_SHA512)
                .build())
            val kp = kpg.generateKeyPair()
            val spki = kp.public.encoded
            val sig = Signature.getInstance("SHA256withECDSA").run {
                initSign(kp.private); update(REKEY_MESSAGE.toByteArray() + spki); sign()
            }
            val certPem = BoxClient.deviceRekey(ctx, spki, sig)
            if (certPem == null || !parseCert(certPem).publicKey.encoded.contentEquals(spki)) {
                try { ks().deleteEntry(alias) } catch (_: Exception) { }
                return false // the next unlock tries again
            }
            // two steps, each committed: the certificate kept beside the key, then the switch
            BoxConfig.writeSecret(ctx, "$ALIAS.cert.next", certPem, now = true)
            p.edit().putString("next_alias", alias).commit()
            finishSwitch(ctx)
            confirm(ctx)
        } catch (e: Exception) {
            android.util.Log.w("LocalGhost", "device key rotation: ${e.javaClass.simpleName} ${e.message}")
            false
        }
    }

    /** The new key and its certificate become the ones presented. */
    private fun finishSwitch(ctx: Context) {
        val p = prefs(ctx)
        val next = p.getString("next_alias", "") ?: ""
        if (next.isEmpty()) return
        val certPem = BoxConfig.readSecret(ctx, "$ALIAS.cert.next")
        val hasKey = try { ks().containsAlias(next) } catch (_: Exception) { false }
        if (certPem == null || !hasKey) {
            p.edit().remove("next_alias").commit()
            BoxConfig.removeSecret(ctx, "$ALIAS.cert.next")
            return
        }
        BoxConfig.writeSecret(ctx, "$ALIAS.cert", certPem, now = true)
        p.edit().putString("alias", next).remove("next_alias").putBoolean("confirm_pending", true).commit()
        BoxConfig.removeSecret(ctx, "$ALIAS.cert.next")
        BoxHttp.reset() // the next connection presents the new key
    }

    /** Over the new certificate: the box retires the QR's. Then the old key leaves the Keystore. */
    private suspend fun confirm(ctx: Context): Boolean {
        if (!BoxClient.deviceRekeyConfirm(ctx)) return false // both certificates work until the next unlock
        dropKeystoreKeys(except = activeAlias(ctx))
        prefs(ctx).edit().putBoolean("rotated", true).remove("confirm_pending").commit()
        android.util.Log.i("LocalGhost", "device key rotated: this phone now presents a key it made, and the QR's is retired")
        return true
    }

    private fun dropKeystoreKeys(except: String) {
        try {
            val ks = ks()
            for (a in ks.aliases().toList()) if (a.startsWith(KS_PREFIX) && a != except) ks.deleteEntry(a)
        } catch (_: Exception) { }
    }

    /** Wrap the stored identity as an X509KeyManager for BoxTrust.socketFactory. */
    fun keyManager(ctx: Context): X509KeyManager? {
        val id = load(ctx) ?: return null
        return SingleIdentityKeyManager(id)
    }

    private fun parseCert(pem: String): X509Certificate {
        val der = pemBody(pem, "CERTIFICATE")
        val cf = CertificateFactory.getInstance("X.509")
        return cf.generateCertificate(ByteArrayInputStream(der)) as X509Certificate
    }

    private fun parseKey(pem: String): PrivateKey {
        val der = pemBody(pem, "PRIVATE KEY")
        // EC keys (the box uses P-256); fall back to RSA if ever needed.
        return try {
            KeyFactory.getInstance("EC").generatePrivate(PKCS8EncodedKeySpec(der))
        } catch (e: Exception) {
            KeyFactory.getInstance("RSA").generatePrivate(PKCS8EncodedKeySpec(der))
        }
    }

    private fun pemBody(pem: String, label: String): ByteArray {
        val base64 = pem
            .replace("-----BEGIN $label-----", "")
            .replace("-----END $label-----", "")
            .replace("\\s".toRegex(), "")
        return Base64.decode(base64, Base64.DEFAULT)
    }

    /** A KeyManager that always presents the one device identity (we have exactly one). */
    private class SingleIdentityKeyManager(private val id: Identity) : X509KeyManager {
        private val alias = "device"
        override fun chooseClientAlias(keyType: Array<out String>?, issuers: Array<out Principal>?, socket: Socket?) = alias
        override fun getCertificateChain(alias: String?) = arrayOf(id.certificate)
        override fun getPrivateKey(alias: String?) = id.privateKey
        override fun getClientAliases(keyType: String?, issuers: Array<out Principal>?) = arrayOf(alias)
        // Server-side methods unused on the phone.
        override fun chooseServerAlias(keyType: String?, issuers: Array<out Principal>?, socket: Socket?): String? = null
        override fun getServerAliases(keyType: String?, issuers: Array<out Principal>?): Array<String>? = null
    }
}
