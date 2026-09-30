package com.localghost.app.local
import org.junit.Assert.*
import org.junit.Test
class PhoneBenchTest {
    private val run = PhoneBench.Run(1_790_000_000_000, "gemma-4-e2b-q2.gguf", 5200, true, 450, 5000, 160, 13000, 4, "Samsung SM-S936B · SM8750 · 8 cores")
    @Test fun speedsAndFeel() {
        assertEquals(90.0, run.readTps, 0.01)
        assertEquals(12.3, run.writeTps, 0.05)
        assertEquals(250 / 90.0 + 200 / 12.3077, run.seconds(250, 200), 0.05)
        val l = PhoneBench.lines(run).toMap()
        assertEquals("90 tokens/s (450 tokens in 5.0 s)", l["reads"])
        assertEquals("12.3 tokens/s (160 tokens in 13 s)", l["writes"])
        assertEquals("5.2 s", l["loads in"])
        assertTrue(l["on"]!!.endsWith("4 threads"))
    }
    @Test fun keptRunsRoundTrip() {
        val runs = List(10) { run.copy(at = run.at + it) }
        val back = PhoneBench.fromJson(PhoneBench.toJson(runs))
        assertEquals(PhoneBench.KEEP, back.size)
        assertEquals(runs.first(), back.first())
        assertTrue(PhoneBench.fromJson("garbage").isEmpty())
        assertTrue(PhoneBench.short(run).contains("reads 90 · writes 12.3 tok/s · gemma-4-e2b-q2"))
        assertTrue("the reading passage is long", PhoneBench.READ_PROMPT.length > 1500)
    }
}
