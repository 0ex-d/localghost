package com.localghost.app.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TrailQuestionTest {
    private val clock = { _: Long -> "14:10" }

    @Test fun readsTheBoxsQuestions() {
        val a = org.json.JSONObject("""{"q":[{"from":1790000000,"to":1790000000,"ts":[1790000000],"lat":39.5,"lon":20.3,
            "awayM":38400,"goneS":720,"kmh":240,"kind":"fast","place":"Igoumenitsa, Greece"},{"from":0,"to":1,"ts":[]}]}""").optJSONArray("q")
        val q = TrailQuestion.listFrom(a)
        assertEquals(1, q.size) // the second is not a question
        assertEquals("fast", q[0].kind)
        assertEquals(1790000000L, q[0].ts[0])
    }

    @Test fun asksInPlainWords() {
        val fast = TrailQuestion(1, 1, longArrayOf(1), 0.0, 0.0, 38_400.0, 720, 240.0, "fast", "Igoumenitsa, Greece")
        assertEquals("At 14:10 the trail goes to Igoumenitsa, Greece, 38 km away and is back 12 min later. That would take 240 km/h. Were you there?",
            fast.text(clock))
        val sea = TrailQuestion(1, 1, longArrayOf(1), 0.0, 0.0, 8_200.0, 0, 20.0, "offroad", "")
        assertEquals("At 14:10 the trail goes to a place 8.2 km away and the day ends there. No road goes there. Were you there?", sea.text(clock))
        val glitch = TrailQuestion(1, 1, longArrayOf(1), 0.0, 0.0, 60_000.0, 5400, 300.0, "glitch", "Bedford")
        assertTrue(glitch.text(clock).contains("back 1 h 30 min later. It is already left off the map."))
    }
}
