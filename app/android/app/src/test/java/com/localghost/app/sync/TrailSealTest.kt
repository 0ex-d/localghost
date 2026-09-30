package com.localghost.app.sync

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TrailSealTest {
    @Test fun sealsAndOpens() {
        val k = TrailSeal.generate()
        assertEquals(32, k.pub.size)
        assertEquals(32, k.priv.size)
        assertTrue(k.pub.contentEquals(TrailSeal.publicOf(k.priv)))
        val line = TrailSeal.seal(k.pub, "1790000000 38.2 20.6 12")
        assertTrue(line.startsWith("s1:"))
        assertFalse("the place shows through", line.contains("38.2"))
        assertEquals("1790000000 38.2 20.6 12", TrailSeal.open(k.priv, line))
        // two seals of one point differ (a new one-off key each time)
        assertFalse(line == TrailSeal.seal(k.pub, "1790000000 38.2 20.6 12"))
    }

    @Test fun anotherKeyOrATamperedLineDoesNotOpen() {
        val k = TrailSeal.generate()
        val line = TrailSeal.seal(k.pub, "1790000000 38.2 20.6")
        assertNull(TrailSeal.open(TrailSeal.generate().priv, line))
        val raw = java.util.Base64.getDecoder().decode(line.substring(3))
        raw[raw.size - 1] = (raw[raw.size - 1].toInt() xor 1).toByte()
        assertNull(TrailSeal.open(k.priv, "s1:" + java.util.Base64.getEncoder().encodeToString(raw)))
        assertNull(TrailSeal.open(k.priv, "1790000000 38.2 20.6"))
        assertNull(TrailSeal.open(k.priv, "s1:not base64 at all"))
    }

    // secd sealed this (secd/trailkey_test.go writes the same vector the other way): the phone opens it
    @Test fun opensWhatTheBoxSealed() {
        val priv = TrailSeal.unb64(BOX_PRIV)!!
        assertEquals(BOX_PLAIN, TrailSeal.open(priv, BOX_LINE))
    }

    companion object {
        const val BOX_PRIV = "CLrjEFrEUmx56aPiJa79Dy9bSSRhkPEA5j1tDeGDCnQ="
        const val BOX_LINE = "s1:powbliH+Y8NN6qaqkwlt6LVPSl5lCSqcKPEiPnNDbGVc/FArSbvTBMPx6UtQkf8XvGh5JE33dwpjFyW06rryKH8ghsoJMmr5Bpx6klfYpLoUlTsrJe+PUQ=="
        const val BOX_PLAIN = "1790003600 51.5007 -0.1246 7"
    }
}
