package com.localghost.app.net

import org.junit.Assert.assertEquals
import org.junit.Test

class FollowUpTest {
    private val before = listOf("hello", "what is the Btc to usd current rate?")

    @Test fun aBareFollowUpAsksThePreviousQuestionAgain() {
        assertEquals("what is the Btc to usd current rate", FollowUp.standalone("how about now?", before))
        assertEquals("what is the Btc to usd current rate", FollowUp.standalone("and now?", before))
        assertEquals("what is the Btc to usd current rate", FollowUp.standalone("same again", before))
    }

    @Test fun aFollowUpKeepsWhatItAdds() {
        assertEquals("what is the Btc to usd current rate euros", FollowUp.standalone("and in euros?", before))
        assertEquals("what is the Btc to usd current rate ethereum", FollowUp.standalone("what about ethereum?", before))
    }

    @Test fun aNewQuestionStandsAsItIs() {
        assertEquals("what is the weather in Lisbon tomorrow?", FollowUp.standalone("what is the weather in Lisbon tomorrow?", before))
        assertEquals("how about the train times from Paddington to Bath tomorrow?",
            FollowUp.standalone("how about the train times from Paddington to Bath tomorrow?", before))
        assertEquals("how about now?", FollowUp.standalone("how about now?", emptyList()))
    }
}
