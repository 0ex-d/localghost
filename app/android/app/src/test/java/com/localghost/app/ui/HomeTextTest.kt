package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class HomeTextTest {
    private val now = 1_790_900_000L

    @Test fun homeShowsBtcAndEthOnly() {
        val coins = listOf(HomeText.liveOnly("SOL", 117.6, 2.0), HomeText.liveOnly("ETH", 2691.59, -1.2), HomeText.liveOnly("BTC", 84497.68, 0.94))
        assertEquals(listOf("BTC", "ETH"), HomeText.pinned(coins).map { it.symbol })
        assertEquals("84,498", HomeText.money(84497.68))
        assertEquals("117.60", HomeText.money(117.6))
        assertEquals("0.2512", HomeText.money(0.25123))
        assertEquals("+0.9%", HomeText.change(0.94))
        assertEquals("-1.2%", HomeText.change(-1.2))
        assertEquals("", HomeText.change(null))
        assertEquals("1.69T", HomeText.cap(1.69e12))
        assertEquals("320.0B", HomeText.cap(3.2e11))
    }

    @Test fun cryptoListTakesTheBoxsOwnPrice() {
        val ranks = listOf(
            HomeText.Coin(2, "ETH", "Ethereum", 2690.0, -1.0, 3.2e11),
            HomeText.Coin(1, "BTC", "Bitcoin", 84000.0, 0.5, 1.69e12),
            HomeText.Coin(3, "XRP", "XRP", 1.49, 2.0, 8.9e10),
        )
        val live = mapOf("BTC" to (84497.68 to 0.94), "ETH" to (2691.59 to null))
        val rows = HomeText.merge(ranks, live, n = 2)
        assertEquals(listOf("BTC", "ETH"), rows.map { it.symbol })
        assertEquals(84497.68, rows[0].usd, 1e-9)
        assertEquals(0.94, rows[0].change24!!, 1e-9)
        assertEquals(-1.0, rows[1].change24!!, 1e-9) // the box had no change for ETH: the list's
    }

    @Test fun briefAge() {
        assertEquals("", HomeText.written(0, now))
        assertEquals("written just now", HomeText.written(now - 30, now))
        assertEquals("written 23 min ago", HomeText.written(now - 23 * 60, now))
        assertEquals("written 3 h ago", HomeText.written(now - 3 * 3600, now))
    }
}
