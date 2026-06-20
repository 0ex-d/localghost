package com.localghost.app.security

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import android.util.Base64
import java.io.File
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.Signature
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.security.spec.ECGenParameterSpec

/**
 * The phone's transport identity. EC P-256, born in StrongBox, non-exportable.
 * NOT auth-gated: background sync must present it without a biometric prompt; its
 * protection is unextractability, not a PIN. The phone exports only its public key
 * and a signature over the enrolment challenge; ghost.secd (Go) mints the cert.
 */
object DeviceIdentity {
    private const val ALIAS = "lg_device_key"
    private const val CERT_FILE = "device.crt"
    private const val KS = "AndroidKeyStore"

    private fun ks() = KeyStore.getInstance(KS).apply { load(null) }

    fun ensureKey() {
        if (ks().containsAlias(ALIAS)) return
        try { generate(strongBox = true) }
        catch (e: StrongBoxUnavailableException) { generate(strongBox = false) }
    }

    private fun generate(strongBox: Boolean) {
        val spec = KeyGenParameterSpec.Builder(
            ALIAS, KeyProperties.PURPOSE_SIGN or KeyProperties.PURPOSE_VERIFY)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setIsStrongBoxBacked(strongBox)
            .build()
        KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, KS)
            .apply { initialize(spec) }
            .generateKeyPair()
    }

    /** X.509 SubjectPublicKeyInfo (DER). Go: x509.ParsePKIXPublicKey(der). */
    fun publicKeyDer(): ByteArray = ks().getCertificate(ALIAS).publicKey.encoded
    fun publicKeyB64(): String = Base64.encodeToString(publicKeyDer(), Base64.NO_WRAP)

    /** Proof of possession over the enrolment challenge. SHA256withECDSA. */
    fun sign(challenge: ByteArray): ByteArray {
        val entry = ks().getEntry(ALIAS, null) as KeyStore.PrivateKeyEntry
        return Signature.getInstance("SHA256withECDSA").run {
            initSign(entry.privateKey); update(challenge); sign()
        }
    }

    fun storeSignedCert(ctx: Context, der: ByteArray) =
        File(ctx.filesDir, CERT_FILE).writeBytes(der)

    fun signedCert(ctx: Context): X509Certificate? {
        val f = File(ctx.filesDir, CERT_FILE)
        if (!f.exists()) return null
        return CertificateFactory.getInstance("X.509")
            .generateCertificate(f.inputStream()) as X509Certificate
    }

    fun isProvisioned(ctx: Context) = File(ctx.filesDir, CERT_FILE).exists()
}
