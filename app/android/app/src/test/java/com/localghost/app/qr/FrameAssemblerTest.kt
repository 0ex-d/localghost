package com.localghost.app.qr

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.security.MessageDigest

/**
 * Pins the app side of the multi-frame QR contract. The frames these tests build are byte-identical
 * to what internal/pair/qrchunk.go emits (same MAGIC, same "MAGIC seq total sha8 chunk" layout, same
 * SHA-256 truncation), so if the box changes the format and the app does not, these fail.
 */
class FrameAssemblerTest {

    private fun sha8(s: String): String {
        val d = MessageDigest.getInstance("SHA-256").digest(s.toByteArray(Charsets.UTF_8))
        return (0 until 4).joinToString("") { "%02x".format(d[it]) }
    }

    /** Mirror of ChunkLink, so the test drives exactly the box's output. */
    private fun chunk(link: String, size: Int = 240): List<String> {
        val c = sha8(link)
        val total = maxOf(1, (link.length + size - 1) / size)
        return (1..total).map { seq ->
            val start = (seq - 1) * size
            val end = minOf(start + size, link.length)
            "LGQR1 $seq $total $c ${link.substring(start, end)}"
        }
    }

    @Test fun roundTripInOrder() {
        val link = "localghost://enroll?v=2&host=192.168.1.50&port=8443&fp=AB12CD34&cert=" +
            "Q".repeat(900) + "&key=" + "Z".repeat(340) + "&name=box"
        val frames = chunk(link)
        assert(frames.size > 1)
        val asm = FrameAssembler()
        var out: String? = null
        for (f in frames) out = asm.offer(f)
        assertEquals(link, out)
    }

    @Test fun outOfOrderAndDuplicates() {
        val link = "localghost://enroll?v=2&host=10.0.0.9&fp=AA&cert=" + "C".repeat(700)
        val frames = chunk(link).shuffled() + chunk(link).first()
        val asm = FrameAssembler()
        var out: String? = null
        for (f in frames) { val r = asm.offer(f); if (r != null) out = r }
        assertEquals(link, out)
    }

    @Test fun incompleteReturnsNull() {
        val link = "localghost://enroll?v=2&host=10.0.0.9&fp=AA&cert=" + "C".repeat(700)
        val frames = chunk(link)
        val asm = FrameAssembler()
        // feed all but the last , must never complete
        for (i in 0 until frames.size - 1) assertNull(asm.offer(frames[i]))
    }

    @Test fun singleFramePassThroughIsNotAFrame() {
        val asm = FrameAssembler()
        assert(!asm.isFrame("localghost://enroll?host=10.0.0.1&fp=CD"))
    }

    @Test fun mismatchedSetResets() {
        val a = chunk("localghost://enroll?v=2&host=1.1.1.1&fp=AA&cert=" + "C".repeat(700))
        val b = chunk("localghost://enroll?v=2&host=2.2.2.2&fp=BB&cert=" + "D".repeat(700))
        val asm = FrameAssembler()
        asm.offer(a[0])
        asm.offer(b[1]) // different set , must not silently merge
        // completing set b now works because the mismatch reset to b's checksum
        var out: String? = null
        for (f in b) out = asm.offer(f)
        assertEquals(b.joinToString("") { it.split(" ", limit = 5)[4] }, out)
    }
}
