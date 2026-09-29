package com.localghost.app.local

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TransferRateTest {
    @Test fun measuresOverTheWindowNotSinceTheStart() {
        val r = TransferRate(windowMs = 4_000, smoothing = 1.0) // no smoothing: the raw window rate
        var t = 0L; var done = 1_000_000_000L // a resume: 1 GB already on the phone
        r.add(t, done)
        assertEquals("under a second: unknown", -1L, r.bytesPerSecond())
        repeat(10) { t += 500; done += 1_000_000; r.add(t, done) } // 2 MB/s
        assertEquals(2_000_000L, r.bytesPerSecond())
        repeat(20) { t += 500; done += 5_000_000; r.add(t, done) } // Wi-Fi got better: 10 MB/s
        assertEquals("the old samples have left the window", 10_000_000L, r.bytesPerSecond())
    }

    @Test fun smoothsTheShownRate() {
        val r = TransferRate(windowMs = 2_000, smoothing = 0.35)
        var t = 0L; var done = 0L
        repeat(6) { t += 500; done += 1_000_000; r.add(t, done) } // 2 MB/s
        val before = r.bytesPerSecond()
        t += 500; done += 10_000_000; r.add(t, done) // one burst
        val after = r.bytesPerSecond()
        assertTrue("moved toward the burst: $before -> $after", after > before)
        assertTrue("but not all the way: $after", after < 8_000_000)
    }

    @Test fun timeLeft() {
        val r = TransferRate(windowMs = 4_000, smoothing = 1.0)
        r.add(0, 0); r.add(2_000, 20_000_000) // 10 MB/s
        assertEquals(100L, r.secondsLeft(20_000_000, 1_020_000_000))
        assertEquals(0L, r.secondsLeft(2_000_000_000, 1_000_000_000))
        assertEquals(-1L, TransferRate().secondsLeft(0, 100))
    }

    @Test fun words() {
        assertEquals("measuring…", TransferRate.rate(-1))
        assertEquals("850 KB/s", TransferRate.rate(850_400))
        assertEquals("12.4 MB/s", TransferRate.rate(12_380_000))
        assertEquals("", TransferRate.left(-1))
        assertEquals("a few seconds left", TransferRate.left(3))
        assertEquals("about 45 s left", TransferRate.left(41))
        assertEquals("about 3 min left", TransferRate.left(121))
        assertEquals("about 1 h left", TransferRate.left(3_600))
        assertEquals("about 1 h 12 min left", TransferRate.left(4_300))
        assertEquals("2.19 GB", TransferRate.size(2_186_000_000))
        assertEquals("640 MB", TransferRate.size(640_000_000))
    }
}
