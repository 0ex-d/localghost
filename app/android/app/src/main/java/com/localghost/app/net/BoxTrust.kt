package com.localghost.app.net

import java.net.Socket
import java.security.MessageDigest
import java.security.cert.X509Certificate
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.X509KeyManager
import javax.net.ssl.X509TrustManager

/**
 * The TLS trust layer for talking to the box. Two halves of the mutual-TLS handshake:
 *
 *  - SERVER trust (pinning): the box is its own CA and presents a self-signed cert. We do NOT trust
 *    the public CA store; we trust exactly the box cert whose SHA-256 matches the fingerprint the
 *    phone read from the enrollment QR. Anything else is rejected. This is stronger than a public CA
 *    (no CA can mis-issue for us) and is why a self-signed box cert is fine.
 *  - CLIENT identity (mTLS): we present the device certificate the box issued and delivered through
 *    the QR. nginx rejects any connection without a box-issued client cert at the handshake, so the
 *    device cert is the access key, checked before any account or PIN.
 *
 * No HTTP library and no public trust anchors. Built on the platform javax.net.ssl only.
 */
object BoxTrust {

    /**
     * Build a socket factory that pins the box server cert by fingerprint and presents the device
     * cert/key for mTLS. pinnedFingerprint is the QR's `fp` (uppercase colon-hex or bare hex).
     * deviceKeyManager supplies the device cert + private key (from secure storage; see DeviceCert).
     */
    fun socketFactory(pinnedFingerprint: String, deviceKeyManager: X509KeyManager): SSLSocketFactory {
        val ctx = SSLContext.getInstance("TLS")
        ctx.init(
            arrayOf(deviceKeyManager),
            arrayOf(PinningTrustManager(normalise(pinnedFingerprint))),
            null,
        )
        return ctx.socketFactory
    }

    /** SHA-256 of a cert's DER, as uppercase colon-separated hex, matching the box and the QR. */
    fun fingerprintOf(cert: X509Certificate): String {
        val sha = MessageDigest.getInstance("SHA-256").digest(cert.encoded)
        return sha.joinToString(":") { "%02X".format(it) }
    }

    private fun normalise(fp: String): String =
        fp.uppercase().filter { it.isLetterOrDigit() }.chunked(2).joinToString(":")

    /**
     * Trusts exactly one server cert: the one whose fingerprint matches the pin. It ignores the
     * system trust store entirely, so a mis-issued public cert or a MITM with a "valid" cert is
     * still rejected , only the box's own key is accepted.
     */
    private class PinningTrustManager(private val pinned: String) : X509TrustManager {
        override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) {
            // The phone is the client; it never validates client certs.
            throw UnsupportedOperationException("client validation happens on the box")
        }

        override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {
            val leaf = chain?.firstOrNull()
                ?: throw java.security.cert.CertificateException("no server certificate presented")
            val actual = fingerprintOf(leaf)
            if (!constantTimeEquals(actual, pinned)) {
                throw java.security.cert.CertificateException(
                    "box certificate does not match the pinned fingerprint; refusing to connect")
            }
            // Matches the pin: trusted. No CA-chain check, the pin IS the trust anchor.
        }

        override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
    }

    /** Constant-time string compare so a near-miss can't be timed out character by character. */
    private fun constantTimeEquals(a: String, b: String): Boolean {
        if (a.length != b.length) return false
        var diff = 0
        for (i in a.indices) diff = diff or (a[i].code xor b[i].code)
        return diff == 0
    }
}
