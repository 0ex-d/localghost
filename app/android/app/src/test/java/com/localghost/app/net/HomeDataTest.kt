package com.localghost.app.net

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class HomeDataTest {
    private val raw = """{"at":1790000000,
        "prices":{"BTC":{"price":101000,"change24":2.5,"hasChange":true,"at":1789999998,"n":6},"ETH":{"price":4000,"hasChange":false,"at":1789999950,"n":5},"SOL":{"price":0}},
        "marketCode":"CRYPTO50","marketValue":1234.5,"marketChange":-1.5,
        "brief":"- Rates held.","briefAt":1789999000,"briefStories":[7],
        "top":[{"id":7,"title":"Rates held","lead":"The bank held rates.","outlets":["BBC","Reuters"],"sources":5,"lastSeen":1789999900}],
        "forYou":{"at":1790000000,"from":1789999280,
          "places":[{"name":"Kensington Gardens","kind":"park","km":1.24,"bearing":"W","why":"you photograph parks","new":true}],
          "days":[{"day":"2024-10-02","yearsAgo":2,"memoryId":0,"title":"","lead":"","photos":24,"place":"Kassiopi"},
                  {"day":"2025-10-02","yearsAgo":1,"memoryId":42,"title":"A walk","lead":"Hyde Park.","photos":0}],
          "stories":[{"id":9,"title":"Blockchain bill","lead":"It passed.","why":"you mention blockchain"}],
          "remember":{"id":42,"kind":"insight","title":"Further on foot","body":"Vlad walked 84 km."}}}"""

    @Test fun parsesTheSnapshot() {
        val s = HomeData.parse(JSONObject(raw))!!
        assertEquals(2, s.prices.size)
        assertEquals(2.5, s.prices["BTC"]!!.change24!!, 1e-9)
        assertNull(s.prices["ETH"]!!.change24)
        assertEquals("CRYPTO50", s.marketCode)
        assertEquals(listOf(7L), s.briefStories)
        assertEquals(listOf("BBC", "Reuters"), s.top[0].outlets)
        val f = s.forYou!!
        assertEquals("Kensington Gardens", f.places[0].name)
        assertEquals("park · 1.2 km W", HomeData.placeLine(f.places[0]))
        assertEquals("day:2024-10-02", HomeData.dayTarget(f.days[0]))
        assertEquals("day:2025-10-02", HomeData.dayTarget(f.days[1]))
        assertEquals("24 photos · Kassiopi", HomeData.dayLine(f.days[0]))
        assertEquals("", HomeData.dayLine(f.days[1]))
        assertEquals("you mention blockchain", f.stories[0].why)
        assertEquals("Further on foot", f.remember!!.title)
        assertEquals("your box noticed", HomeData.rememberHeading(f.remember!!.kind))
        assertEquals("remembered", HomeData.rememberHeading("distilled"))
        assertNull(HomeData.parse(JSONObject("{}")))
        assertNull(HomeData.parse(null))
    }

    @Test fun forYouStaysOffTheDisk() {
        val s = HomeData.parse(JSONObject(raw))!!
        val disk = HomeData.forDisk(s)
        assertFalse(disk.contains("Kensington"))
        assertFalse(disk.contains("forYou"))
        val back = HomeData.parse(JSONObject(disk))!!
        assertNull(back.forYou)
        assertEquals(s.copy(forYou = null), back)
    }

    @Test fun wordsAndTheNewerBrief() {
        val s = HomeData.parse(JSONObject(raw))!!
        assertTrue(HomeData.newerBrief(1789998000, s))
        assertFalse(HomeData.newerBrief(1789999000, s))
        assertFalse(HomeData.newerBrief(0, null))
        assertEquals("1 year ago", HomeData.yearsAgo(1))
        assertEquals("3 years ago", HomeData.yearsAgo(3))
        assertEquals("350 m", HomeData.distance(0.34))
        assertEquals("12 km", HomeData.distance(12.4))
        assertEquals("near you · from your trail 12 min ago", HomeData.nearFrom(1789999280, 1790000000))
        assertEquals("near you", HomeData.nearFrom(0, 1790000000))
    }
}
