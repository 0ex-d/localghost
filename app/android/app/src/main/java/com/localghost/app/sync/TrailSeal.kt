package com.localghost.app.sync

import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.PrivateKey
import java.security.PublicKey
import java.security.SecureRandom
import java.security.spec.PKCS8EncodedKeySpec
import java.security.spec.X509EncodedKeySpec
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.KeyAgreement
import javax.crypto.Mac
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

/**
 * A point of the trail, sealed. The phone seals every point it records to a public key; the
 * private half lives in the box's vault (or, on a phone with no box, behind the phone's own lock),
 * so the spool and the last two days on this phone read as noise until the app is unlocked.
 * Sealing needs only the public key, so the background worker seals while the app is locked and
 * holds nothing that opens what it wrote.
 *
 * A sealed line is "s1:" and base64 of: a one-off X25519 public key (32 bytes), a nonce (12), and
 * AES-256-GCM of the point's text with "s1:" as additional data. The AES key is HKDF-SHA256 of
 * the X25519 secret, salted with the one-off and the recipient's public keys, info "localghost
 * trail v1". The box's side is secd/trailkey.go; the two are checked against each other in tests.
 *
 * Keys travel as their raw 32 bytes (base64): the platform's encodings of X25519 differ in detail
 * between the phone and a JVM, the raw bytes do not.
 *
 * Pure (javax.crypto only), so the JVM tests run it as it runs on the phone.
 */
object TrailSeal {
    const val PREFIX = "s1:"
    private const val INFO = "localghost trail v1"
    private val SPKI = byteArrayOf(0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x03, 0x21, 0x00)
    private val PKCS8 = byteArrayOf(0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x04, 0x22, 0x04, 0x20)

    /** A key pair as raw bytes: [pub] and [priv], 32 each. */
    class Keys(val pub: ByteArray, val priv: ByteArray)

    fun generate(): Keys {
        val kp = KeyPairGenerator.getInstance("XDH").generateKeyPair()
        return Keys(rawPublic(kp.public), rawPrivate(kp.private))
    }

    /** The public half of a raw private key. */
    fun publicOf(priv: ByteArray): ByteArray {
        // X25519 of the base point: agree with the key whose u-coordinate is 9
        val nine = ByteArray(32).also { it[0] = 9 }
        return agree(priv, nine)
    }

    fun isSealed(line: String): Boolean = line.startsWith(PREFIX)

    /** [plain] sealed to [pub]. */
    fun seal(pub: ByteArray, plain: String, rnd: SecureRandom = SecureRandom()): String {
        val kp = KeyPairGenerator.getInstance("XDH").generateKeyPair()
        val ephPub = rawPublic(kp.public)
        val ka = KeyAgreement.getInstance("XDH")
        ka.init(kp.private)
        ka.doPhase(publicKey(pub), true)
        val shared = ka.generateSecret()
        val nonce = ByteArray(12).also { rnd.nextBytes(it) }
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.ENCRYPT_MODE, SecretKeySpec(hkdf(shared, ephPub + pub), "AES"), GCMParameterSpec(128, nonce))
        c.updateAAD(PREFIX.toByteArray())
        val ct = c.doFinal(plain.toByteArray(Charsets.UTF_8))
        shared.fill(0)
        return PREFIX + Base64.getEncoder().encodeToString(ephPub + nonce + ct)
    }

    /** The text of a sealed line, or null when it does not open with [priv] (or is not one). */
    fun open(priv: ByteArray, line: String): String? = Opener(priv).open(line)

    /** An opener for many lines: the public half worked out once. */
    class Opener(private val priv: ByteArray) {
        private val pub = publicOf(priv)
        fun open(line: String): String? {
            if (!isSealed(line)) return null
            val raw = try { Base64.getDecoder().decode(line.substring(PREFIX.length).trim()) } catch (_: IllegalArgumentException) { return null }
            if (raw.size < 32 + 12 + 16) return null
            val ephPub = raw.copyOfRange(0, 32)
            return try {
                val shared = agree(priv, ephPub)
                val c = Cipher.getInstance("AES/GCM/NoPadding")
                c.init(Cipher.DECRYPT_MODE, SecretKeySpec(hkdf(shared, ephPub + pub), "AES"), GCMParameterSpec(128, raw, 32, 12))
                c.updateAAD(PREFIX.toByteArray())
                shared.fill(0)
                String(c.doFinal(raw, 44, raw.size - 44), Charsets.UTF_8)
            } catch (_: Exception) {
                null
            }
        }
    }

    private fun agree(priv: ByteArray, pub: ByteArray): ByteArray {
        val ka = KeyAgreement.getInstance("XDH")
        ka.init(privateKey(priv))
        ka.doPhase(publicKey(pub), true)
        return ka.generateSecret()
    }

    /** HKDF-SHA256 (RFC 5869), one block: extract with [salt], expand with the info. */
    internal fun hkdf(ikm: ByteArray, salt: ByteArray): ByteArray {
        val ext = Mac.getInstance("HmacSHA256")
        ext.init(SecretKeySpec(salt, "HmacSHA256"))
        val prk = ext.doFinal(ikm)
        val exp = Mac.getInstance("HmacSHA256")
        exp.init(SecretKeySpec(prk, "HmacSHA256"))
        exp.update(INFO.toByteArray())
        exp.update(1)
        return exp.doFinal()
    }

    fun publicKey(raw: ByteArray): PublicKey =
        KeyFactory.getInstance("XDH").generatePublic(X509EncodedKeySpec(SPKI + raw))

    fun privateKey(raw: ByteArray): PrivateKey =
        KeyFactory.getInstance("XDH").generatePrivate(PKCS8EncodedKeySpec(PKCS8 + raw))

    /** The raw 32 bytes of a public key (its X.509 encoding ends with them). */
    fun rawPublic(k: PublicKey): ByteArray = k.encoded.let { it.copyOfRange(it.size - 32, it.size) }

    /** The raw 32 bytes of a private key: the octet string inside its PKCS#8 encoding (04 22 04 20). */
    fun rawPrivate(k: PrivateKey): ByteArray {
        val e = k.encoded
        for (i in 0..e.size - 36) {
            if (e[i] == 0x04.toByte() && e[i + 1] == 0x22.toByte() && e[i + 2] == 0x04.toByte() && e[i + 3] == 0x20.toByte())
                return e.copyOfRange(i + 4, i + 36)
        }
        throw IllegalStateException("not an X25519 PKCS#8 key")
    }

    fun b64(b: ByteArray): String = Base64.getEncoder().encodeToString(b)
    fun unb64(s: String): ByteArray? = try { Base64.getDecoder().decode(s) } catch (_: IllegalArgumentException) { null }
}
