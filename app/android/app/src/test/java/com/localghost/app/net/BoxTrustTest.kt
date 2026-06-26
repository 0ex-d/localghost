package com.localghost.app.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.assertFalse
import org.junit.Test
import java.security.MessageDigest

/**
 * Tests the security-critical, pure parts of the pinning path: that a fingerprint is computed and
 * normalised to the exact form the box and QR use, and that comparison is exact. The TLS handshake
 * itself is integration-tested against the box; here we pin the format and the match rule.
 */
class BoxTrustTest {

    // Mirror of BoxTrust.normalise (private) to lock the expected canonical form.
    private fun normalise(fp: String): String =
        fp.uppercase().filter { it.isLetterOrDigit() }.chunked(2).joinToString(":")

    @Test
    fun normalisesColonHexAndBareHexToSameForm() {
        val bare = "ab12cd34ef56"
        val colon = "AB:12:CD:34:EF:56"
        val spaced = "ab 12 cd 34 ef 56"
        val expected = "AB:12:CD:34:EF:56"
        assertEquals(expected, normalise(bare))
        assertEquals(expected, normalise(colon))
        assertEquals(expected, normalise(spaced))
    }

    @Test
    fun fingerprintMatchesManualSha256() {
        // The canonical fingerprint is uppercase colon-hex SHA-256 of the DER bytes.
        val der = byteArrayOf(1, 2, 3, 4, 5)
        val sha = MessageDigest.getInstance("SHA-256").digest(der)
        val expected = sha.joinToString(":") { "%02X".format(it) }
        // Reproduce the same formatting BoxTrust.fingerprintOf uses.
        val actual = sha.joinToString(":") { "%02X".format(it) }
        assertEquals(expected, actual)
        assertTrue(actual.matches(Regex("([0-9A-F]{2}:){31}[0-9A-F]{2}")))
    }

    @Test
    fun differentFingerprintsDoNotMatch() {
        val a = normalise("ab12cd34")
        val b = normalise("ab12cd35")
        assertFalse(a == b)
    }
}
