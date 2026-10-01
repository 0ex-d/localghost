package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class UnreadableTextTest {
    @Test fun row() {
        assertEquals("1 Oct 2026 12:00 UTC · 4.2 MB · missing 0xff00 sequence",
            UnreadableText.row(1_790_856_000L, 4_200_000, "damaged: missing 0xff00 sequence; ffmpeg cannot read it either (Picture size 29476x44915 is invalid)"))
        assertEquals("1 Oct 2026 12:00 UTC · 900 KB", UnreadableText.row(1_790_856_000L, 900_000, ""))
    }
}
