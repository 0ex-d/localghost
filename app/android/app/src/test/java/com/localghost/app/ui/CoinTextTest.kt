package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class CoinTextTest {
    @Test fun thinKeepsTheEnds() {
        val pts = (0 until 1000).toList()
        val t = CoinText.thin(pts, 100)
        assertEquals(100, t.size)
        assertEquals(0, t.first())
        assertEquals(999, t.last())
        assertEquals(5, CoinText.thin(listOf(1, 2, 3, 4, 5), 240).size)
    }

    @Test fun figuresAndHowTheBoxMadeThePrice() {
        assertEquals(10.0, CoinText.changeOver(listOf(100.0, 105.0, 110.0))!!, 1e-9)
        assertNull(CoinText.changeOver(listOf(100.0)))
        assertEquals("19.9M BTC", CoinText.supply(19_900_000.0, "BTC"))
        assertEquals("1.20B DOGE", CoinText.supply(1.2e9, "DOGE"))
        assertEquals("$1.73T", CoinText.dollars(1.73e12))
        assertEquals("from 9 markets on 6 exchanges · USD, USDT, BTC", CoinText.madeFrom(9, 6, "USD,USDT,BTC"))
        assertEquals("from 1 exchange · USD", CoinText.madeFrom(1, 1, "USD"))
        assertEquals("<1%", CoinText.share(0.004))
        assertEquals("42%", CoinText.share(0.42))
        assertEquals("kraken · SOL/BTC", CoinText.market("kraken", "SOL", "BTC"))
        assertEquals(0xFF9945FFL, CoinText.color("#9945FF"))
        assertNull(CoinText.color("purple"))
        assertEquals("written by your box from Wikipedia, Coinbase and solana.com", CoinText.writtenBy("Solana is…", "Wikipedia, Coinbase, solana.com", "x"))
        assertEquals("written by your box from Coinbase", CoinText.writtenBy("Solana is…", "Coinbase", ""))
        assertEquals("Coinbase's description", CoinText.writtenBy("", "", "Fast."))
        assertEquals("", CoinText.writtenBy("", "", ""))
    }

    @Test fun memoryKindsAndTheNote() {
        val kinds = listOf("person", "person", "me", "day", "episode", "user", "distilled")
        val c = MemoryKinds.counts(kinds)
        assertEquals(7, c["all"])
        assertEquals(2, c["people"])
        assertEquals(2, c["days"])
        assertEquals(true, MemoryKinds.matches("people", "person"))
        assertEquals(false, MemoryKinds.matches("people", "me"))
        assertEquals("you are Vlad · 3 about you · 4 people", AboutText.status("Vlad", 3, 4, false, true))
        assertEquals("nothing written yet", AboutText.status("", 0, 0, false, false))
    }
}
