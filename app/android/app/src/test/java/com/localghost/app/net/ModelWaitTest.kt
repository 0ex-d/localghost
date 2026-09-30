package com.localghost.app.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ModelWaitTest {
    @Test fun readsTheBoxsAnswer() {
        val m = ModelWait.Box.fromJson(org.json.JSONObject("""{"ready":false,"phase":"weights","pct":42,"etaMs":8000,"elapsedMs":6000}"""))!!
        assertFalse(m.ready)
        assertEquals("weights", m.phase)
        assertEquals(42, m.pct)
        assertEquals(8_000L, m.etaMs)
        assertTrue(ModelWait.Box.fromJson(org.json.JSONObject("""{"ready":true,"phase":"ready","pct":100}"""))!!.ready)
    }

    @Test fun anOlderBoxSaysNothing() {
        // an older box has no /v1/model; whatever comes back is not a model state and chat sends at once
        assertNull(ModelWait.Box.fromJson(org.json.JSONObject("""{"ok":true}""")))
        assertNull(ModelWait.Box.fromJson(null))
    }

    @Test fun theLineSaysWhatHowFarAndHowLong() {
        val l = ModelWait.boxLine(ModelWait.Box(false, "weights", 42, 8_000, ""))
        assertTrue(l, l.startsWith("your box is loading its model into the GPU · 42% · about 10 s left"))
        // no percent outside the weights, no time left when the box cannot tell
        val s = ModelWait.boxLine(ModelWait.Box(false, "starting", 0, -1, "the model service is starting"))
        assertFalse(s, s.contains("%") || s.contains("left"))
        assertFalse(ModelWait.boxLine(ModelWait.Box(false, "failed", 0, 5_000, "")).contains("left"))
    }

    @Test fun thePhoneLineUsesTheLastLoad() {
        assertEquals("loading the phone's model…", ModelWait.phoneLine(200, 0))
        assertEquals("loading the phone's model · 3 s…", ModelWait.phoneLine(3_200, 0))
        assertEquals("loading the phone's model · a few seconds left", ModelWait.phoneLine(4_000, 6_000))
        assertTrue(ModelWait.phoneLine(9_000, 6_000).contains("longer than last time"))
    }
}
