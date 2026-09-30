package com.localghost.app.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UnlockPacerTest {
    private var clock = 0L
    private fun pacer(min: Long = 700) = UnlockPacer(minStepMs = min, now = { clock })

    private fun snap(vararg done: UnlockStage, allDone: Boolean = false): UnlockSnapshot {
        val states = HashMap<UnlockStage, StageState>()
        for (d in done) states[d] = StageState.COMPLETE
        if (allDone) UnlockStage.order.forEach { states[it] = states[it] ?: StageState.COMPLETE }
        val rows = UnlockStage.order.map { StageProgress(it, states[it] ?: StageState.PENDING) }
        return UnlockSnapshot(rows, done = allDone || states[UnlockStage.READY] == StageState.COMPLETE)
    }

    private fun state(s: UnlockSnapshot, st: UnlockStage) = s.stages.first { it.stage == st }.state

    // A warm box finishes every step in one snapshot; the pacer must still walk the six rings one at
    // a time, one per minStep, and only then report done.
    @Test fun warmUnlockIsWalkedOneStepAtATime() {
        val p = pacer(700)
        clock = 0
        p.observe(snap(allDone = true)) // the box says: all done, right now
        // at t=0 nothing is shown done yet; RESOLVE is running
        var d = p.display()!!
        assertEquals(StageState.RUNNING, state(d, UnlockStage.RESOLVE))
        assertEquals(StageState.PENDING, state(d, UnlockStage.UNSEAL))
        assertFalse(d.done)

        clock = 700; p.observe(snap(allDone = true)); d = p.display()!!
        assertEquals(StageState.COMPLETE, state(d, UnlockStage.RESOLVE))
        assertEquals(StageState.RUNNING, state(d, UnlockStage.UNSEAL))
        assertFalse(d.done)

        // after the sixth ring's min-step the rings are all lit; READY and skipped MODEL add nothing
        clock = 6 * 700; p.observe(snap(allDone = true)); d = p.display()!!
        UnlockStage.order.filter { it != UnlockStage.MODEL && it != UnlockStage.READY }.forEach {
            assertEquals("$it lit", StageState.COMPLETE, state(d, it))
        }
        assertTrue("done once the rings are shown", d.done)
    }

    // A step the box really sits on keeps its real length, not shrunk to the minimum.
    @Test fun aSlowStepKeepsItsRealLength() {
        val p = pacer(700)
        clock = 0; p.observe(snap()) // RESOLVE running
        clock = 500; p.observe(snap(UnlockStage.RESOLVE)) // RESOLVE done at 500 (< min)
        // MOUNT has not arrived; UNSEAL runs. The box sits on it.
        clock = 5000; p.observe(snap(UnlockStage.RESOLVE, UnlockStage.UNSEAL))
        val d = p.display()!!
        // UNSEAL shown done no earlier than its real time (5000), well past the 700 minimum
        assertEquals(StageState.COMPLETE, state(d, UnlockStage.UNSEAL))
        assertEquals(StageState.RUNNING, state(d, UnlockStage.MOUNT))
    }

    // A failure shows the real snapshot at once, no pacing (the person sees where it stopped).
    @Test fun aFailurePassesThrough() {
        val p = pacer(700)
        clock = 0; p.observe(snap())
        val rows = UnlockStage.order.map {
            StageProgress(it, if (it == UnlockStage.MOUNT) StageState.ERRORED
            else if (it == UnlockStage.RESOLVE || it == UnlockStage.UNSEAL) StageState.COMPLETE
            else StageState.PENDING)
        }
        p.observe(UnlockSnapshot(rows, done = false, failed = "failed at mounting store"))
        val d = p.display()!!
        assertEquals("failed at mounting store", d.failed)
        assertEquals(StageState.ERRORED, state(d, UnlockStage.MOUNT))
    }

    @Test fun theFloorIsSixRingSteps() {
        assertEquals(6 * 720L, UnlockPacer.FLOOR_MS)
    }
}
