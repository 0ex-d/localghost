package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class MapCountryTextTest {
    @Test fun rowsSayWhatTheBoxHolds() {
        assertEquals("Greece · 1,234 streets · 40 main · 31 coast · 312 MB", MapCountryText.row("Greece", 1234, 40, 31, 312_000_000))
        assertEquals("Andorra · 3 streets · 1 main · 120 KB", MapCountryText.row("Andorra", 3, 1, 0, 120_000))
        assertEquals("Chile · nothing on the box for it", MapCountryText.row("Chile", 0, 0, 0, 0))
        assertEquals("1.5 GB", MapCountryText.size(1_500_000_000))
    }

    @Test fun progressCountsTiles() {
        assertEquals("Greece · 812 of 1,274 tiles on this phone (312 MB in all)", MapCountryText.progress("Greece", 812, 1274, 312_000_000))
        assertEquals("Greece · all 1,274 tiles on this phone (312 MB)", MapCountryText.progress("Greece", 1274, 1274, 312_000_000))
        assertEquals("Chile · the box has no tiles for it yet", MapCountryText.progress("Chile", 0, 0, 0))
    }

    @Test fun pickedInAFewWords() {
        assertEquals("no whole country picked", MapCountryText.picked(emptyList()))
        assertEquals("Greece", MapCountryText.picked(listOf("Greece")))
        assertEquals("Greece and Italy", MapCountryText.picked(listOf("Greece", "Italy")))
        assertEquals("Greece, Italy and 2 more", MapCountryText.picked(listOf("Greece", "Italy", "Spain", "France")))
    }
}
