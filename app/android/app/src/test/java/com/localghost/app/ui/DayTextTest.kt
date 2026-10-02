package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.TimeZone

class DayTextTest {
    private val london = TimeZone.getTimeZone("Europe/London")

    @Test fun daysAndYearsEitherSide() {
        assertEquals("2025-10-01", DayText.shift("2025-10-02", -1, london))
        assertEquals("2026-01-01", DayText.shift("2025-12-31", 1, london))
        assertEquals("2024-10-02", DayText.shiftYears("2025-10-02", -1, london))
        assertEquals("2023-02-28", DayText.shiftYears("2024-02-29", -1, london))
        assertEquals("Thursday 2 October 2025", DayText.heading("2025-10-02", london))
        assertEquals("2025-10-02", DayText.of(1759395600, london)) // 2025-10-02 09:00 UTC
        val (a, b) = DayText.bounds("2025-10-02", london)
        assertEquals(1759359600L, a) // midnight BST is 23:00 UTC the day before
        assertEquals(86400L, b - a)
        assertTrue(DayText.touches(a + 3600, a + 7200, "2025-10-02", london))
        assertTrue(DayText.touches(a - 86400, b + 86400, "2025-10-02", london))
        assertFalse(DayText.touches(b + 10, b + 20, "2025-10-02", london))
    }

    @Test fun howLongAgo() {
        assertEquals("today", DayText.ago("2026-10-02", "2026-10-02"))
        assertEquals("yesterday", DayText.ago("2026-10-01", "2026-10-02"))
        assertEquals("1 year ago", DayText.ago("2025-10-02", "2026-10-02"))
        assertEquals("3 years ago", DayText.ago("2023-10-02", "2026-10-02"))
        assertEquals("5 days ago", DayText.ago("2026-09-27", "2026-10-02"))
        assertEquals("3 weeks ago", DayText.ago("2026-09-10", "2026-10-02"))
        assertEquals("over a year ago", DayText.ago("2025-09-01", "2026-10-02"))
        assertEquals("tomorrow", DayText.ago("2026-10-03", "2026-10-02"))
    }

    @Test fun linesUnderTheParts() {
        assertEquals("24 photos · 1 video", DayText.media(24, 1))
        assertEquals("", DayText.media(0, 0))
        assertEquals("8,412 steps · slept 7 h 20 min · 45 min of exercise", DayText.body(8412, 440, 45))
        assertEquals("slept 8 h", DayText.body(0, 480, 0))
        assertEquals("this day has not happened yet", DayText.nothing("2026-10-05", "2026-10-02"))
    }

    @Test fun mapHeights() {
        assertEquals("climbed 420 m · highest 1,240 m up", MapText.heights(420.4, 1240.0))
        assertEquals("highest 12 m up", MapText.heights(5.0, 12.0))
        assertEquals("", MapText.heights(0.0, 0.0))
    }
}
