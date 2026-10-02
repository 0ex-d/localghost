package com.localghost.app.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class NotifLinkTest {
    @Test fun theBoxsLinkWins() {
        assertEquals(NotifLink.Target("map", "2026-09-28"), NotifLink.resolve("map:2026-09-28", "ghost.framed", "highlight"))
        assertEquals(NotifLink.Target("memories", "4182"), NotifLink.resolve("memories:4182", "ghost.cued", "reflection"))
        assertEquals(NotifLink.Target("memories", "near"), NotifLink.resolve("memories:near", "ghost.cued", "nearby"))
        assertEquals(NotifLink.Target("news"), NotifLink.resolve("news", "ghost.synthd", "news"))
        assertEquals(NotifLink.Target("status"), NotifLink.resolve("status", "ghost.watchd", "down"))
        // a day or an id that is not one is dropped, the place stays
        assertEquals(NotifLink.Target("map", ""), NotifLink.resolve("map:yesterday", "", ""))
        assertEquals(NotifLink.Target("memories", ""), NotifLink.resolve("memories:", "", ""))
    }

    @Test fun anOlderNotificationGoesByItsKind() {
        assertEquals(NotifLink.Target("map"), NotifLink.resolve("", "ghost.framed", "highlight"))
        assertEquals(NotifLink.Target("memories"), NotifLink.resolve("", "ghost.cued", "reflection"))
        assertEquals(NotifLink.Target("memories", "near"), NotifLink.resolve("", "ghost.cued", "nearby"))
        assertEquals(NotifLink.Target("memories"), NotifLink.resolve("", "ghost.secd", "checkin"))
        assertEquals(NotifLink.Target("news"), NotifLink.resolve("", "ghost.synthd", "news"))
        assertEquals(NotifLink.Target("status"), NotifLink.resolve("", "ghost.shadowd", "observation"))
        assertEquals(NotifLink.Target(""), NotifLink.resolve("", "ghost.noted", "message"))
    }

    @Test fun theShadesExtra() {
        assertEquals("map:2026-09-28", NotifLink.nav("map:2026-09-28", "ghost.framed", "highlight"))
        assertEquals("memories", NotifLink.nav("", "ghost.secd", "checkin"))
        assertEquals("notifications", NotifLink.nav("", "ghost.noted", "message"))
        assertEquals("open on MAP ›", NotifText.opens(NotifLink.Target("map", "2026-09-28")))
        assertEquals("open near you ›", NotifText.opens(NotifLink.Target("memories", "near")))
    }
}
