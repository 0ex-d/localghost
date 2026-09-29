package com.localghost.app.checkin

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class FeelingsTest {
    @Test fun everyOldFeelingIsStillOffered() {
        // the twelve the card offered before 29 Sep 2026: past check-ins keep meaning something
        for (f in listOf("calm", "happy", "energised", "grateful", "focused", "proud",
                "tired", "stressed", "anxious", "restless", "low", "lonely")) {
            assertTrue(f, f in Feelings.all)
        }
        assertEquals("no feeling in two groups", Feelings.all.size, Feelings.all.toSet().size)
        // and every guess the box can make (hw.DayContext) is a feeling here
        for (f in listOf("tired", "rested", "energised", "calm", "curious", "stressed")) assertTrue(f, f in Feelings.all)
    }

    @Test fun usualCountsAcrossCheckins() {
        val u = Feelings.usual(listOf("calm, tired", "tired", "(unspecified)", "Happy, tired, calm", ""))
        assertEquals(listOf("tired", "calm", "happy"), u)
    }

    @Test fun quickRowIsGuessesThenUsualThenCommon() {
        val q = Feelings.quick(listOf("rested", "curious"), listOf("calm", "rested", "proud"))
        assertEquals(listOf("rested", "curious", "calm", "proud", "happy", "tired", "stressed", "focused"), q)
        assertEquals(Feelings.QUICK, q.size)
    }

    @Test fun preselectAndToggle() {
        assertEquals(listOf("tired", "rested"), Feelings.preselect(listOf("tired", "rested", "energised")))
        var p = Feelings.preselect(listOf("tired"))
        p = Feelings.toggle(p, "tired")                // untick the guess
        assertEquals(emptyList<String>(), p)
        for (f in listOf("a", "b", "c", "d", "e")) p = Feelings.toggle(p, f)
        assertEquals(listOf("a", "b", "c", "d"), p)    // four at most
    }

    @Test fun checkinTextKeepsTheLinesTheBoxParses() {
        val t = Feelings.checkinText("2026-09-29", listOf("calm", "tired"), listOf("tired"), "  a long swim ",
            "0123456789abcdef0123456789abcdef", 72_400)
        assertEquals("Daily check-in 2026-09-29\nFeeling: calm, tired\nPreselected: tired\nWhy: a long swim\n" +
            "Voice: 0123456789abcdef0123456789abcdef 1:12", t)
        assertEquals("Daily check-in 2026-09-29\nFeeling: (unspecified)",
            Feelings.checkinText("2026-09-29", emptyList(), emptyList(), "", null, 0))
    }

    @Test fun rowsFitTheWidth() {
        val rows = Feelings.rows(listOf("disappointed", "overwhelmed", "calm", "low", "sad", "tired", "happy"))
        for (r in rows) assertTrue(r.toString(), r.sumOf { it.length + 3 } <= 30 || r.size == 1)
        assertEquals(7, rows.sumOf { it.size })
        assertEquals(listOf("disappointed", "overwhelmed"), rows[0])
    }
}
