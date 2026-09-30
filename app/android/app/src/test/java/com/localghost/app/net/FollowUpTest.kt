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

    @Test fun aPrivateQuestionIsNotBorrowedForTheWeb() {
        val fresh = { q: String -> Regex("\\b(today|now|weather)\\b").containsMatchIn(q) } // the auto rule, in miniature
        // "and now?" after a private question: the question would not have gone to the web itself
        org.junit.Assert.assertFalse(FollowUp.mayBorrow(listOf("what did I talk about with Maria at the cafe on Tuesday?"), fresh))
        // after a public one it may
        org.junit.Assert.assertTrue(FollowUp.mayBorrow(listOf("what's the weather in Loggos today?"), fresh))
        org.junit.Assert.assertTrue(FollowUp.mayBorrow(emptyList(), fresh))
    }

    @Test fun questionsAboutThePersonAreSeen() {
        for (q in listOf("how many times did I go to the gym this month?", "who is my dentist?", "where were we on Tuesday", "what did Maria tell me"))
            org.junit.Assert.assertTrue(q, FollowUp.looksPersonal(q))
        for (q in listOf("what's the weather in Loggos today?", "EUR to GBP rate", "who is the prime minister of Greece", "Imagine Dragons tour dates"))
            org.junit.Assert.assertFalse(q, FollowUp.looksPersonal(q))
    }
}
