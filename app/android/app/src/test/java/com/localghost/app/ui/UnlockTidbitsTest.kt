package com.localghost.app.ui

import com.localghost.app.net.ModelLoad
import com.localghost.app.net.UnlockStage
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class UnlockTidbitsTest {
    @Test fun evenTicksSayWhatIsHappeningOddOnesATip() {
        val m = ModelLoad("weights", 42, 9000, 4000)
        assertEquals("> loading the model's weights into the GPU: 42%", UnlockTidbits.line(0, UnlockStage.MODEL, m, 0))
        assertTrue(UnlockTidbits.line(1, UnlockStage.MODEL, m, 0).startsWith("did you know? "))
        assertEquals("> Postgres wakes up inside the encrypted store", UnlockTidbits.line(2, UnlockStage.START_DB, null, 0))
    }

    @Test fun tipsTurnThroughAllOfThemAndTheSeedMovesTheStart() {
        val n = UnlockTidbits.tips.size
        val seen = (0 until 2 * n).filter { it % 2 == 1 }.map { UnlockTidbits.line(it, UnlockStage.MODEL, null, 3) }.toSet()
        assertEquals(n, seen.size)
        assertTrue(UnlockTidbits.line(1, null, null, 0) != UnlockTidbits.line(1, null, null, 1))
        assertTrue("negative seeds are fine", UnlockTidbits.line(1, null, null, -7).startsWith("did you know?"))
    }

    @Test fun everyStageHasItsOwnWords() {
        val lines = UnlockStage.values().map { UnlockTidbits.doing(it, null) }
        assertEquals(lines.size, lines.toSet().size)
        assertEquals("starting the model engine", UnlockTidbits.doing(UnlockStage.MODEL, null))
        assertEquals("loading the model's weights into the GPU", UnlockTidbits.doing(UnlockStage.MODEL, ModelLoad("weights", 0, -1, 0)))
    }
}
