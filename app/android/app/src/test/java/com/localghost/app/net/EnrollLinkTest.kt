package com.localghost.app.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertNotNull
import org.junit.Test

/**
 * Guards the enrollment-link parser. This is the QR trust anchor, so the tests pin that it refuses
 * anything without a host, code, AND fingerprint rather than enrolling insecurely.
 */
class EnrollLinkTest {

    @Test fun parsesFullLink() {
        val link = EnrollLink.parse(
            "localghost://enroll?host=192.168.1.20&port=8443&fp=ab:cd:ef&name=xyntai")
        assertNotNull(link)
        link!!
        assertEquals("192.168.1.20", link.host)
        assertEquals(8443, link.port)
        assertEquals("xyntai", link.boxName)
        assertEquals("https://192.168.1.20:8443", link.baseUrl())
    }

    @Test fun ignoresUnknownParams() {
        // A leftover or future param (e.g. the retired pairing code) must not break parsing.
        val link = EnrollLink.parse("localghost://enroll?host=h&code=LEGACY-1234&fp=aa:bb")
        assertNotNull(link)
    }

    @Test fun defaultsPortWhenAbsent() {
        val link = EnrollLink.parse("localghost://enroll?host=box.local&fp=aa:bb")
        assertNotNull(link)
        assertEquals(8443, link!!.port)
    }

    @Test fun normalisesFingerprint() {
        val link = EnrollLink.parse("localghost://enroll?host=h&fp=abcdef")
        assertEquals("AB:CD:EF", link!!.certFingerprint)
    }

    @Test fun rejectsMissingFingerprint() {
        assertNull(EnrollLink.parse("localghost://enroll?host=h"))
    }

    @Test fun rejectsMissingHost() {
        assertNull(EnrollLink.parse("localghost://enroll?fp=aa:bb"))
    }

    @Test fun rejectsWrongScheme() {
        assertNull(EnrollLink.parse("https://enroll?host=h&fp=aa"))
        assertNull(EnrollLink.parse("random text"))
        assertNull(EnrollLink.parse(""))
    }

    @Test fun rejectsBadPort() {
        assertNull(EnrollLink.parse("localghost://enroll?host=h&fp=aa&port=99999"))
    }

    @Test fun linkWithoutCertOrKeyStillParses() {
        // Hand-typed path: cert/key are optional at parse time; the enrol flow is what requires them.
        val link = EnrollLink.parse("localghost://enroll?host=h&fp=aa:bb")
        assertNotNull(link)
        assertNull(link!!.deviceCertDer)
        assertNull(link.deviceKeyDer)
    }

    @Test fun v1LinkCarriesCertAndKeyDer() {
        // Raw DER is binary: use bytes across the full range so a UTF-8 round-trip bug (which
        // corrupts anything >= 0x80) cannot pass. Encoded unpadded base64url, as the box emits.
        val certDer = ByteArray(380) { (it * 7).toByte() }
        val keyDer = ByteArray(138) { (it * 7).toByte() }
        val enc = java.util.Base64.getUrlEncoder().withoutPadding()
        val link = EnrollLink.parse(
            "localghost://enroll?v=1&host=h&port=8443&fp=aa:bb" +
                "&cert=${enc.encodeToString(certDer)}&key=${enc.encodeToString(keyDer)}")
        assertNotNull(link)
        assertEquals(1, link!!.version)
        org.junit.Assert.assertArrayEquals(certDer, link.deviceCertDer)
        org.junit.Assert.assertArrayEquals(keyDer, link.deviceKeyDer)
    }

    @Test fun newerVersionReturnsOutdated() {
        // v1 is the current (and first published) format, so any higher number is a box from the
        // future: the scanner must say "update the app", not mis-parse or reject as garbage.
        val res = EnrollLink.parseResult("localghost://enroll?v=2&host=h&fp=aa:bb")
        assertEquals(EnrollLink.Result.Outdated(2), res)
        assertNull(EnrollLink.parse("localghost://enroll?v=2&host=h&fp=aa:bb"))
    }
}
