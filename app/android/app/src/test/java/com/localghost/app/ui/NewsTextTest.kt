package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class NewsTextTest {
    private val now = 1_790_900_000L

    @Test fun outletsAndAges() {
        assertEquals("BBC · The Guardian · 2 h ago", NewsText.outlets(listOf("BBC", "The Guardian", "BBC"), now - 7200, now))
        assertEquals("A · B · C · 2 more · just now", NewsText.outlets(listOf("A", "B", "C", "D", "E"), now - 10, now))
        assertEquals("never", NewsText.ago(0, now))
        assertEquals("3 days ago", NewsText.ago(now - 3 * 86400, now))
    }

    @Test fun statusLine() {
        assertEquals("nothing fetched yet , the phone fetches the feeds every two hours while it is on", NewsText.status(0, 0, now, false))
        assertEquals("feeds fetched 5 min ago · last digest 3 h ago", NewsText.status(now - 300, now - 3 * 3600, now, false))
        assertEquals("fetching the feeds on this phone…", NewsText.status(1, 1, now, true))
    }

    @Test fun marketsLine() {
        assertEquals("BTC 65,000 USD (4 venues, 5 min ago) · ETH 3,250 USD (3 venues, 5 min ago) · USDT 0.9991 · EUR/GBP 0.8712 · EUR/RON 5.0800 · ECB 2026-09-30",
            NewsText.markets(listOf(NewsText.Price("ETH", 3250.0, 3, now - 300), NewsText.Price("USDT", 0.9991, 2, now - 300), NewsText.Price("BTC", 65000.0, 4, now - 300)),
                mapOf("GBP" to 0.8712, "RON" to 5.08), "2026-09-30", now))
        assertEquals("no market numbers on the box yet", NewsText.markets(emptyList(), emptyMap(), "", now))
        assertEquals("crypto 1,234.5 (+1.23% today, 50 coins)", NewsText.market(1234.5, 1.234, 50))
        assertEquals("", NewsText.market(0.0, 0.0, 0))
        assertEquals("crypto 1,234.5 (-0.50% today, 50 coins) · EUR/GBP 0.8712",
            NewsText.markets(emptyList(), mapOf("GBP" to 0.8712), "", now, NewsText.market(1234.5, -0.5, 50)))
        assertEquals("0.0001", NewsText.money(0.0001234))
    }

    @Test fun aSummaryIsALeadAndPoints() {
        val t = NewsText.told("The Bank kept its rate at 4%.\n\n- Inflation is cooling.\n• The vote was close\nand split the committee.\n2. The next decision is in November.")
        assertEquals("The Bank kept its rate at 4%.", t.lead)
        assertEquals(listOf("Inflation is cooling.", "The vote was close and split the committee.", "The next decision is in November."), t.points)
        assertEquals("A paragraph from before. Its second line.", NewsText.told("A paragraph from before.\nIts second line.").lead)
        assertEquals("The Bank kept its rate at 4%.", NewsText.lead("The Bank kept its rate at 4%.\n- Inflation is cooling."))
        assertEquals("only a point", NewsText.lead("- only a point"))
        assertEquals(true, NewsText.wantsFetch(0, now))
        assertEquals(false, NewsText.wantsFetch(now - 600, now))
        assertEquals(true, NewsText.wantsFetch(now - 1800, now))
    }
}
