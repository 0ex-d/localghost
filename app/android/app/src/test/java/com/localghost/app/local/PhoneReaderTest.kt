package com.localghost.app.local

import com.localghost.app.net.WebSearch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Who reads the pages, how much of each, and which of a small model's notes survive: the decisions
 * of the phone/box split, without a phone.
 */
class PhoneReaderTest {
    private val P = PhoneReader.Plan
    private val gpu = WebSearch.BoxSpeed(known = true, onGPU = true, promptTps = 2300.0, genTps = 58.0)
    private val cpu = WebSearch.BoxSpeed(known = true, onGPU = false, promptTps = 35.0, genTps = 3.0)

    @Test fun whoReads() {
        // three pages of ~1500 tokens each, a phone reading 80 t/s and writing 14 t/s
        val route = { reach: Boolean, box: WebSearch.BoxSpeed?, planned: Boolean, usable: Boolean ->
            P.route(reach, box, planned, usable, 80.0, 14.0, 3, 4500)
        }
        assertEquals(PhoneReader.Route.BOX_READS, route(true, gpu, true, true))      // the GPU reads it all in 2 s
        assertEquals(PhoneReader.Route.PHONE_READS, route(true, cpu, false, true))   // 4500/35 = 129 s on the box CPU
        assertEquals(PhoneReader.Route.BOX_READS, route(true, cpu, false, false))    // no phone model: the box, slow or not
        assertEquals(PhoneReader.Route.PHONE_READS, route(true, null, false, true))  // the box said nothing in time
        assertEquals(PhoneReader.Route.BOX_READS, route(true, null, true, true))     // an old box that planned but gave no speed
        assertEquals(PhoneReader.Route.PHONE_ALONE, route(false, null, false, true))
        assertEquals(PhoneReader.Route.NOBODY, route(false, null, false, false))
        // a CPU box with little to read: sending the pages is quicker than reading them here
        assertEquals(PhoneReader.Route.BOX_READS, P.route(true, cpu, false, true, 80.0, 14.0, 1, 300))
        // a painfully slow phone loses to a CPU box
        assertEquals(PhoneReader.Route.BOX_READS, P.route(true, cpu, false, true, 8.0, 2.0, 3, 4500))
        // no pages (weather, a rate): nothing to read
        assertEquals(PhoneReader.Route.BOX_READS, P.route(true, cpu, false, true, 80.0, 14.0, 0, 0))
    }

    @Test fun howMuchOfEachPage() {
        // 45 s over 3 pages at 80/14 t/s: 15 s a page less ~8 s of notes -> ~560 tokens
        val per = P.perPageTokens(3, 80.0, 14.0)
        assertTrue("$per", per in 500..620)
        assertEquals(P.PAGE_TOKENS_MAX, P.perPageTokens(1, 400.0, 30.0))
        assertEquals(0, P.perPageTokens(8, 20.0, 4.0)) // too slow to read anything: quotes only
        assertEquals(0, P.perPageTokens(0, 80.0, 14.0))
        val paras = listOf("First paragraph about coffee in Athens, long enough to count as prose.", "Second paragraph " + "x".repeat(900), "Third")
        val t = P.pageText("The description.", paras, 100) // ~400 chars: the long paragraph is cut into the room left
        assertTrue(t, t.startsWith("The description.\n\nFirst paragraph") && t.length <= 401 && t.endsWith("…"))
        // a sliver under 200 characters is not worth a cut paragraph
        val u = P.pageText("The description.", paras, 60)
        assertTrue(u, u.endsWith("count as prose.") && !u.contains("Second"))
    }

    @Test fun notesThatStayInsideThePage() {
        val page = "A freddo espresso costs between 3 and 4.50 euros in central Athens in 2026. The island price is 5 euros. Visitors: 1,200,000 a year."
        val notes = """
            - A freddo espresso costs 3 to 4.50 euros in central Athens (2026)
            - On the islands it is 5 euros
            - A cappuccino costs 6 euros
            - 1200000 visitors a year
            * Prices rose 12% since 2024
            - It is the most popular summer coffee
        """.trimIndent()
        val kept = P.groundNotes(notes, page)
        assertEquals(listOf(
            "A freddo espresso costs 3 to 4.50 euros in central Athens (2026)",
            "On the islands it is 5 euros",
            "1200000 visitors a year",
            "It is the most popular summer coffee",
        ), kept)
        assertTrue(P.groundNotes("NOTHING", page).isEmpty())
        assertTrue(P.groundNotes("nothing useful here.\n", page).isEmpty())
        assertTrue(P.groundNotes("- 99 euros\n- 7 days", page).isEmpty())
        // at most five, each short
        val many = (1..9).joinToString("\n") { "- line without numbers number " + "y".repeat(it) }
        assertEquals(5, P.groundNotes(many, page).size)
        val long = P.groundNotes("- " + "word ".repeat(100), page)
        assertTrue(long.single().length <= 281 && long.single().endsWith("…"))
    }

    @Test fun quoteAndPrompts() {
        val paras = listOf("Welcome to our site about many things.", "The freddo espresso price in Athens is 3 to 4.50 euros this year.", "Subscribe to our newsletter.")
        assertEquals(paras[1], P.bestQuote(paras, "freddo espresso price Athens"))
        assertTrue(P.bestQuote(listOf("z ".repeat(400)), "q").length <= 421)
        val np = P.notePrompt("the price of a freddo in Athens", "Coffee prices", "example.gr", "2026-05-01", "TEXT")
        assertTrue(np.contains("What is needed: the price of a freddo in Athens") && np.contains("Coffee prices , example.gr, 2026-05-01") &&
            np.contains("\"\"\"\nTEXT\n\"\"\"") && np.endsWith("write only: NOTHING"))
        val h = WebSearch.Hit("Coffee prices", "https://example.gr/c", "snip", "- 3 to 4.50 euros", "note", "duckduckgo", "2026-05-01")
        val ap = P.answerPrompt("how much is a freddo?", listOf(h))
        assertTrue(ap, ap.contains("[1] Coffee prices (example.gr, 2026-05-01)\n- 3 to 4.50 euros") && ap.contains("citing the notes by number"))
    }

    @Test fun cleanTheModelsMachinery() {
        assertEquals("The answer.", LocalModel.clean("<|channel>thought\nlet me think<channel|>The answer."))
        assertEquals("", LocalModel.clean("<|channel>thought\nstill thinking when the budget ran out"))
        assertEquals("Hi there.", LocalModel.clean("\n\nHi there.<turn|>"))
        assertEquals("Plain.", LocalModel.clean("Plain.<end_of_turn>"))
        assertEquals(10f, LocalModel.Speed.ewma(0f, 10f))
        assertEquals(13f, LocalModel.Speed.ewma(10f, 20f), 0.001f)
        assertFalse(LocalModel.clean("a < b and c > d").isEmpty())
    }
}
