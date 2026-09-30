package com.localghost.app.security

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.nio.file.Files

class CacheSweepTest {
    @Test fun sweepsTheLeftoversAndNothingElse() {
        val dir = Files.createTempDirectory("sweep").toFile()
        val keep = File(dir, "thumbs.bin").apply { writeText("x") }
        File(dir, "capture_1727650000000.jpg").writeText("photo")
        File(dir, "play-0123abcd.tmp").writeText("video")
        File(dir, "localghost-export.json").writeText("{}")
        File(dir, "voice-play").mkdirs(); File(dir, "voice-play/ab.wav").writeText("wav")
        assertEquals(4, CacheSweep.sweep(dir))
        assertTrue(keep.exists())
        assertFalse(File(dir, "voice-play/ab.wav").exists())
    }

    @Test fun atStartTheFreshOnesStay() {
        val dir = Files.createTempDirectory("sweep").toFile()
        val fresh = File(dir, "capture_2.jpg").apply { writeText("in flight") }
        val stale = File(dir, "capture_1.jpg").apply { writeText("old"); setLastModified(System.currentTimeMillis() - 3_600_000) }
        assertEquals(1, CacheSweep.sweep(dir, minAgeMs = 600_000))
        assertTrue(fresh.exists()); assertFalse(stale.exists())
    }
}
