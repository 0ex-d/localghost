package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TrailStatusTest {
    private val now = 1_790_900_000L

    @Test fun theFix() {
        assertEquals("last fix: 12 min ago", TrailStatus.fixLine(now - 720, now))
        assertEquals("last fix: none this phone can read yet , the next one comes within the quarter hour", TrailStatus.fixLine(null, now))
        assertTrue(TrailStatus.fixLine(null, now, "unreadable").contains("will not open it"))
    }

    @Test fun theSend() {
        assertEquals("to the box: sent 4 · 3 min ago", TrailStatus.sendLine(now - 180, "sent 4", true, now - 180, 0, now))
        assertEquals("to the box: not sent: the box answered HTTP 503 · 3 min ago · last sent 5 h ago",
            TrailStatus.sendLine(now - 180, "not sent: the box answered HTTP 503", false, now - 5 * 3600, 12, now))
        assertEquals("to the box: not sent: the box has no key for this phone's trail yet (handed over at the next PIN unlock) · just now · never sent from this phone",
            TrailStatus.sendLine(now - 10, "not sent: the box has no key for this phone's trail yet (handed over at the next PIN unlock)", false, 0, 30, now))
        assertEquals("to the box: not tried yet , 7 waiting", TrailStatus.sendLine(null, null, null, null, 7, now))
    }

    @Test fun nearFrom() {
        assertEquals("around the box's newest trail point, 2 h ago", TrailStatus.nearFrom(true, now - 7200, now))
        assertEquals("around this phone's last fix, just now", TrailStatus.nearFrom(false, now - 30, now))
    }

    @Test fun todayAndHolds() {
        assertEquals("today: 14 kept (9 quarter-hour · 3 other apps' fixes · 2 app opened) · 12 sent to the box · 2 waiting",
            TrailStatus.todayLine(14, mapOf("w" to 9, "p" to 3, "a" to 2), 12, 2))
        assertEquals("today: 0 kept · 0 sent to the box · none waiting", TrailStatus.todayLine(0, emptyMap(), 0, 0))
        assertEquals("on this phone: 51 points from the last two days · 1204 sent to the box since the trail began", TrailStatus.holdsLine(51, 1204))
        assertEquals("unmarked", TrailStatus.viaName(""))
    }
}
