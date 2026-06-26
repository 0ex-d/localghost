package com.localghost.app.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class UnlockStreamTest {

    private fun stateOf(snap: UnlockSnapshot, stage: UnlockStage): StageState =
        snap.stages.first { it.stage == stage }.state

    @Test
    fun hotAccountShowsEveryStageCompleteAndDone() {
        val states = UnlockStage.order.associateWith { StageState.COMPLETE }
        val snap = UnlockSnapshot.from(states)
        assertTrue(snap.done)
        assertNull(snap.failed)
        UnlockStage.order.forEach { assertEquals(StageState.COMPLETE, stateOf(snap, it)) }
    }

    @Test
    fun coldPartialLeavesUnreachedStagesPending() {
        val snap = UnlockSnapshot.from(
            mapOf(
                UnlockStage.RESOLVE to StageState.COMPLETE,
                UnlockStage.UNSEAL to StageState.COMPLETE,
                UnlockStage.MOUNT to StageState.RUNNING,
            )
        )
        assertFalse(snap.done)
        assertEquals(StageState.PENDING, stateOf(snap, UnlockStage.START_DB))
        assertEquals(StageState.PENDING, stateOf(snap, UnlockStage.READY))
    }

    @Test
    fun readyCompleteMeansDone() {
        val states = UnlockStage.order.associateWith { StageState.COMPLETE }
        assertTrue(UnlockSnapshot.from(states).done)
    }

    @Test
    fun warmStagesSkippedStillReachDone() {
        val snap = UnlockSnapshot.from(
            mapOf(
                UnlockStage.RESOLVE to StageState.COMPLETE,
                UnlockStage.UNSEAL to StageState.SKIPPED,
                UnlockStage.MOUNT to StageState.SKIPPED,
                UnlockStage.START_DB to StageState.SKIPPED,
                UnlockStage.START_CACHE to StageState.SKIPPED,
                UnlockStage.DAEMONS to StageState.COMPLETE,
                UnlockStage.READY to StageState.COMPLETE,
            )
        )
        assertTrue(snap.done)
        assertEquals(StageState.SKIPPED, stateOf(snap, UnlockStage.MOUNT))
    }

    @Test
    fun errorSurfacesAndIsNotDone() {
        val snap = UnlockSnapshot.from(
            mapOf(
                UnlockStage.RESOLVE to StageState.COMPLETE,
                UnlockStage.UNSEAL to StageState.ERRORED,
            )
        )
        assertFalse(snap.done)
        assertTrue(snap.failed!!.contains("unsealing key"))
    }

    @Test
    fun stageOrderIsFixedAndUniform() {
        // The rendered order is always the box's fixed order, for any account.
        val snap = UnlockSnapshot.from(mapOf(UnlockStage.RESOLVE to StageState.RUNNING))
        assertEquals(UnlockStage.order, snap.stages.map { it.stage })
    }
}
