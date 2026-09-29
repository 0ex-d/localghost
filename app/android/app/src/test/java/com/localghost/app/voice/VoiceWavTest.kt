package com.localghost.app.voice

import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.File

class VoiceWavTest {
    private fun hex(b: ByteArray) = b.joinToString("") { "%02x".format(it.toInt() and 0xff) }

    /** The same 44 bytes the box's voiced_test.go asserts (PhoneHeaderHex): the two sides agree. */
    @Test fun headerMatchesTheBox() {
        assertEquals("52494646247d000057415645666d74201000000001000100803e0000007d00000200100064617461007d0000",
            hex(VoiceWav.header(16000, 1, 16, 32000)))
    }

    @Test fun finishWritesTheSizesAndDurationReadsThem() {
        val f = File.createTempFile("voicewav", ".wav")
        try {
            val samples = ShortArray(16000) { i -> (if (i % 2 == 0) 1000 else -32768).toShort() }
            val bytes = ByteArray(samples.size * 2)
            val peak = VoiceWav.pack(samples, samples.size, bytes)
            assertEquals(32768, peak)
            f.writeBytes(VoiceWav.header(16000, 1, 16, 0) + bytes)
            VoiceWav.finish(f)
            val h = f.readBytes().copyOfRange(0, 44)
            assertEquals("52494646247d000057415645666d74201000000001000100803e0000007d00000200100064617461007d0000", hex(h))
            assertEquals(1000L, VoiceWav.durationMs(f))
            // little-endian: 1000 = e8 03, -32768 = 00 80
            assertEquals("e8030080", hex(f.readBytes().copyOfRange(44, 48)))
        } finally {
            f.delete()
        }
    }
}
