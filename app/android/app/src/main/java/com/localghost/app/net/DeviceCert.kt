package com.localghost.app.net

import android.content.Context
import android.util.Base64
import com.localghost.app.security.BoxConfig
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
 * The device identity the box issued and delivered through the enrollment QR: a client certificate
 * and its private key. The box generates these (the phone does not), so enrollment is one scan. The
 * private key is sensitive , the QR that carried it is a credential , so it is stored only in the
 * app's encrypted store (AndroidKeyStore-wrapped, the same scheme as BoxConfig) and surfaced to the
 * TLS stack as an X509KeyManager for the mutual-TLS handshake.
 *
 * Trade noted in the design: because the box generated the key, it existed off-device before the
 * phone held it. The box wipes its copy at the end of setup; the phone keeps the only remaining copy
 * here, encrypted.
 */
object DeviceCert {

    private const val ALIAS = "device-cert"

    /** Parsed device identity. */
    data class Identity(val certificate: X509Certificate, val privateKey: PrivateKey)

    /** Store the PEM cert + PKCS8 private key delivered by the box (via the QR enrollment payload). */
    fun store(ctx: Context, certPem: String, keyPkcs8Pem: String) {
        // Reuse BoxConfig's encrypted blob store for the sensitive material.
        BoxConfig.writeSecret(ctx, "$ALIAS.cert", certPem)
        BoxConfig.writeSecret(ctx, "$ALIAS.key", keyPkcs8Pem)
    }

    /**
     * Store the identity from raw DER (the enrol link carries DER, not PEM , base64url over PEM was
     * ~1.78x the bytes and blew the QR budget). At-rest format stays PEM: wrap here so load(),
     * parseCert/parseKey, and the key manager are untouched, and a stored blob is inspectable text
     * either way.
     */
    fun store(ctx: Context, certDer: ByteArray, keyPkcs8Der: ByteArray) =
        store(ctx, toPem(certDer, "CERTIFICATE"), toPem(keyPkcs8Der, "PRIVATE KEY"))

    /** The exact inverse of pemBody: base64 (RFC 4648 standard alphabet, 64-char lines) in armour. */
    private fun toPem(der: ByteArray, label: String): String {
        val b64 = Base64.encodeToString(der, Base64.NO_WRAP)
        val body = b64.chunked(64).joinToString("\n")
        return "-----BEGIN $label-----\n$body\n-----END $label-----\n"
    }

    fun isEnrolled(ctx: Context): Boolean =
        BoxConfig.readSecret(ctx, "$ALIAS.cert") != null && BoxConfig.readSecret(ctx, "$ALIAS.key") != null

    fun load(ctx: Context): Identity? {
        val certPem = BoxConfig.readSecret(ctx, "$ALIAS.cert") ?: return null
        val keyPem = BoxConfig.readSecret(ctx, "$ALIAS.key") ?: return null
        return try {
            Identity(parseCert(certPem), parseKey(keyPem))
        } catch (e: Exception) {
            null
        }
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
