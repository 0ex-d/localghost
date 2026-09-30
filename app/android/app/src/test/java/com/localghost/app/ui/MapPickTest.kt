package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MapPickTest {
    @Test fun onlyVeryCloseIn() {
        // a unit is about 39 km at the equator, 30 km at Paxos
        assertEquals(39_135.0, MapPick.metresPerUnit(0.0), 5.0)
        assertEquals(30_300.0, MapPick.metresPerUnit(39.24), 100.0)
        // a 1080 px phone on Paxos: 1,000x sees about 30 km, 20,000x about 1.5 km, 40,000x about
        // 800 m, the deepest zoom about 30 m; the line is about a kilometre across the screen
        val base = 1080.0 / MapPick.UNITS
        assertFalse(MapPick.canPick(MapPick.metresPerPx(39.24, base * 1_000)))
        assertFalse(MapPick.canPick(MapPick.metresPerPx(39.24, base * 20_000)))
        assertTrue(MapPick.canPick(MapPick.metresPerPx(39.24, base * 40_000)))
        assertTrue(MapPick.canPick(MapPick.metresPerPx(39.24, base * MapPick.MAX_ZOOM)))
        assertEquals(31.0, 1080 * MapPick.metresPerPx(39.24, base * MapPick.MAX_ZOOM), 1.0)
    }

    @Test fun pickTheNearestFixWithAClock() {
        val xs = doubleArrayOf(100.0, 100.001, 100.002)
        val ys = doubleArrayOf(200.0, 200.0, 200.0)
        val times = longArrayOf(10, 0, 30)
        val pxz = 100_000.0 // 100 px per 0.001 unit
        // the view centred on the middle fix, 1000×1000; taps in px
        fun tap(x: Double) = MapPick.nearest(xs, ys, times, 100.001, 200.0, pxz, 1000.0, 1000.0, x, 500.0, 40.0)
        assertEquals(0, tap(405.0))
        assertEquals(2, tap(590.0))
        assertEquals(-1, tap(500.0)) // the middle fix has no clock
        assertEquals(-1, tap(460.0)) // too far from both
        assertEquals(-1, MapPick.nearest(xs, ys, LongArray(0), 100.001, 200.0, pxz, 1000.0, 1000.0, 405.0, 500.0, 40.0))
    }

    @Test fun saysWhatGoes() {
        val clock = { t: Long -> "t$t" }
        assertEquals("The fix at t5.", MapPick.describe(5, null, clock))
        assertEquals("The fix at t5, on its own at this spot.", MapPick.describe(5, longArrayOf(5), clock))
        assertEquals("The fix at t5, and 2 more at this spot (t4 to t9).", MapPick.describe(5, longArrayOf(4, 5, 9), clock))
    }

    @Test fun labelsDoNotOverlap() {
        val claimed = ArrayList<FloatArray>()
        assertTrue(MapPick.claim(claimed, floatArrayOf(0f, 0f, 100f, 20f)))
        assertFalse(MapPick.claim(claimed, floatArrayOf(50f, 10f, 150f, 30f)))
        assertTrue(MapPick.claim(claimed, floatArrayOf(0f, 25f, 100f, 45f)))
        assertEquals(2, claimed.size)
    }
}
