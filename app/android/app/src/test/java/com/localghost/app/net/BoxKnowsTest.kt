package com.localghost.app.net

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class BoxKnowsTest {
    @Test fun theBoxsNumbersAreNotSearchedFor() {
        listOf("btc price?", "how is ethereum doing today", "ETH", "top 50 cryptos", "biggest coins", "how is crypto doing now",
            "100 euros in pounds", "gbp to ron exchange rate", "convert 50 dollars to eur").forEach {
            assertTrue(it, BoxKnows.covers(it))
        }
        listOf("where is the nearest pharmacy", "who won the match yesterday", "is bitcoin a good idea for a pension in theory",
            "news about the rail strike in france").forEach {
            assertFalse(it, BoxKnows.covers(it))
        }
    }
}
