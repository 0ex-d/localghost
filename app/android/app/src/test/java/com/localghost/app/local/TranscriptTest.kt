package com.localghost.app.local

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TranscriptTest {
    @Test fun noHistoryLeavesThePromptAlone() {
        assertEquals("hello", Transcript.withHistory("hello", emptyList()))
        assertEquals("hello", Transcript.withHistory("hello", listOf(true to "  ")))
    }

    @Test fun theConversationGoesInFrontOldestFirst() {
        val p = Transcript.withHistory("and in euros?", listOf(true to "what is BTC in USD?", false to "About 83,600 USD."))
        assertEquals("The conversation so far, oldest first:\nYou: what is BTC in USD?\nGhost: About 83,600 USD.\n\n" +
            "The next message, to answer as part of that conversation:\nand in euros?", p)
    }

    @Test fun boundedToTheLatestTurns() {
        val h = (1..20).map { (it % 2 == 1) to "turn $it " + "x".repeat(1000) }
        val p = Transcript.withHistory("q", h)
        assertTrue("the newest turn is kept", p.contains("turn 20 "))
        assertTrue("old turns are dropped", !p.contains("turn 1 "))
        assertTrue("within the budget: ${p.length}", p.length < Transcript.MAX_CHARS + 300)
    }
}
