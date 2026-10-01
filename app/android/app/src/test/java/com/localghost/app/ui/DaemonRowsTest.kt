package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class DaemonRowsTest {
    private val rows = listOf(
        DaemonRows.Row("counting since", "2 h ago"),
        DaemonRows.Row("photos named", "40 last hour", key = true),
        DaemonRows.Row("gps image/jpeg", "8 of 40"),
        DaemonRows.Row("behind", "undescribed 12", key = true),
    )

    @Test fun keyRowsFirstRestFolded() {
        val p = DaemonRows.pick(rows, all = false)
        assertEquals(listOf("photos named", "behind"), p.shown.map { it.k })
        assertEquals(2, p.hidden)
    }

    @Test fun allShowsEverythingInOrder() {
        val p = DaemonRows.pick(rows, all = true)
        assertEquals(rows, p.shown)
        assertEquals(0, p.hidden)
    }

    @Test fun unmarkedBoxStillShowsSomething() {
        val plain = (1..8).map { DaemonRows.Row("k$it", "v$it") }
        val p = DaemonRows.pick(plain, all = false)
        assertEquals(DaemonRows.FALLBACK, p.shown.size)
        assertEquals(3, p.hidden)
        assertEquals(0, DaemonRows.pick(emptyList(), all = false).hidden)
    }
}
