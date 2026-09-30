package com.localghost.app.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class UnlockClockTest {
    private var t = 0L
    private fun snap(done: Set<UnlockStage>, running: UnlockStage? = null, model: ModelLoad? = null): UnlockSnapshot {
        val states = HashMap<UnlockStage, StageState>()
        done.forEach { states[it] = StageState.COMPLETE }
        running?.let { states[it] = StageState.RUNNING }
        return UnlockSnapshot.from(states, model)
    }
    private val o = UnlockStage.order
    private fun upTo(st: UnlockStage) = o.takeWhile { it != st }.toSet()

    @Test fun timesEachStepAndLearnsFromAColdUnlock() {
        val c = UnlockClock(now = { t })
        c.observe(snap(emptySet(), UnlockStage.RESOLVE))
        t = 1_000; c.observe(snap(upTo(UnlockStage.MOUNT), UnlockStage.MOUNT))       // resolve+unseal in the first second
        t = 3_000; c.observe(snap(upTo(UnlockStage.START_DB), UnlockStage.START_DB)) // mount took 2 s
        val mid = c.estimate()
        assertEquals(UnlockStage.START_DB, mid.current)
        assertEquals(2_000L, mid.took[UnlockStage.MOUNT])
        assertTrue(mid.fraction > 0f && mid.fraction < 1f)
        t = 4_000; c.observe(snap(upTo(UnlockStage.MODEL), UnlockStage.MODEL))
        t = 24_000; c.observe(snap(o.toSet()))
        val end = c.estimate()
        assertEquals(1f, end.fraction)
        assertEquals(0L, end.secondsLeft)
        assertEquals(20_000L, end.took[UnlockStage.MODEL])
        val learned = c.learn()
        assertNotNull(learned)
        assertEquals(20_000L, learned!![UnlockStage.MODEL.name])
        assertNull("learned once per run", c.learn())

        // the next unlock expects what the last one took: at the start of MODEL, about 20 s left
        t = 100_000
        val next = UnlockClock(learned, now = { t })
        next.observe(snap(upTo(UnlockStage.MODEL), UnlockStage.MODEL))
        val e = next.estimate()
        assertTrue("confident with learned times", e.confident)
        assertEquals(21L, e.secondsLeft) // 20 s model + 0.5 s ready (the learned 100 ms floor), rounded up
    }

    @Test fun theBoxsModelEstimateWinsWhileItLoads() {
        val c = UnlockClock(now = { t })
        c.observe(snap(upTo(UnlockStage.MODEL), UnlockStage.MODEL, ModelLoad("weights", 40, 9_000, 6_000)))
        t = 2_000 // two seconds after the box said 9 s
        val e = c.estimate()
        assertTrue(e.confident)
        assertEquals(8L, e.secondsLeft) // 7 s of model + 0.5 s ready, rounded up
    }

    @Test fun neverGoesBackAndHoldsShortOfTheEnd() {
        val c = UnlockClock(now = { t })
        c.observe(snap(upTo(UnlockStage.MODEL), UnlockStage.MODEL, ModelLoad("weights", 10, 2_000, 1_000)))
        t = 1_500
        val a = c.estimate().fraction
        c.observe(snap(upTo(UnlockStage.MODEL), UnlockStage.MODEL, ModelLoad("weights", 12, 60_000, 2_500))) // a slower estimate
        val b = c.estimate().fraction
        assertTrue("never back: $a -> $b", b >= a)
        t = 600_000
        val late = c.estimate()
        assertTrue("not done until the box says so", late.fraction < 1f)
    }

    @Test fun aStepFarOverItsUsualTimeIsOverdue() {
        val c = UnlockClock(mapOf(UnlockStage.START_DB.name to 1_000L), now = { t })
        c.observe(snap(upTo(UnlockStage.START_DB), UnlockStage.START_DB))
        t = 3_000; assertFalse(c.estimate().overdue)
        t = 45_000
        val e = c.estimate()
        assertTrue(e.overdue)
        assertEquals(45_000L, e.took[UnlockStage.START_DB])
    }

    @Test fun aWarmBoxIsNotLearnedFrom() {
        val c = UnlockClock(now = { t })
        c.observe(UnlockSnapshot.initial())
        t = 800; c.observe(snap(o.toSet()))
        assertTrue(c.estimate().warm)
        assertNull(c.learn())
    }

    @Test fun aFailedUnlockIsNotLearnedFrom() {
        val c = UnlockClock(now = { t })
        c.observe(snap(emptySet(), UnlockStage.RESOLVE))
        t = 5_000
        c.observe(UnlockSnapshot.from(mapOf(UnlockStage.RESOLVE to StageState.COMPLETE, UnlockStage.UNSEAL to StageState.ERRORED)))
        assertNull(c.learn())
    }

    @Test fun modelLoadFromThePoll() {
        val o = org.json.JSONObject("""{"phase":"weights","pct":42,"etaMs":9000,"elapsedMs":4000}""")
        assertEquals(ModelLoad("weights", 42, 9000, 4000), ModelLoad.fromJson(o))
        assertNull(ModelLoad.fromJson(org.json.JSONObject("{}")))
        assertNull(ModelLoad.fromJson(null))
        assertEquals(100, ModelLoad.fromJson(org.json.JSONObject("""{"phase":"ready","pct":140}"""))!!.pct)
    }

    // --- the floor: a quick unlock must not look quick ---

    @Test fun aWarmBoxWalksTheStepsUntilTheFloor() {
        val c = UnlockClock(now = { t })
        c.observe(snap(o.toSet()).copy(floorMs = 8_000)) // ready at the first look
        val a = c.estimate()
        assertEquals(o.first(), a.current)
        assertEquals(8L, a.secondsLeft)
        assertTrue(a.fraction < 0.05f)
        t = 4_000
        val b = c.estimate()
        assertEquals(o[3], b.current) // 4 s of 8, seven steps: the fourth
        assertEquals(4L, b.secondsLeft)
        assertTrue(b.fraction in 0.45f..0.55f)
        assertFalse(b.overdue)
        t = 7_900
        assertEquals(o[o.size - 2], c.estimate().current) // the last step before ready
        t = 8_000
        val end = c.estimate()
        assertNull(end.current)
        assertEquals(1f, end.fraction)
        assertNull("a warm box teaches nothing", c.learn())
    }

    @Test fun paddingNeverStepsBackAndLearnsTheRealTimes() {
        val c = UnlockClock(now = { t })
        c.observe(snap(emptySet(), UnlockStage.RESOLVE).copy(floorMs = 9_000))
        t = 1_000; c.observe(snap(upTo(UnlockStage.MODEL), UnlockStage.MODEL).copy(floorMs = 9_000))
        t = 2_000; c.observe(snap(o.toSet()).copy(floorMs = 9_000))
        t = 2_500
        val p = c.estimate()
        assertEquals("shown MODEL already: stays there", UnlockStage.MODEL, p.current)
        assertEquals(7L, p.secondsLeft)
        t = 9_000
        assertNull(c.estimate().current)
        assertEquals(1_000L, c.learn()!![UnlockStage.MODEL.name])
    }

    @Test fun beforeDoneTheTimeLeftIsNeverUnderTheFloor() {
        val c = UnlockClock(mapOf(UnlockStage.RESOLVE.name to 100L, UnlockStage.UNSEAL.name to 100L,
            UnlockStage.MOUNT.name to 100L, UnlockStage.START_DB.name to 100L, UnlockStage.START_CACHE.name to 100L,
            UnlockStage.DAEMONS.name to 100L, UnlockStage.MODEL.name to 100L, UnlockStage.READY.name to 100L), now = { t })
        c.observe(snap(emptySet(), UnlockStage.RESOLVE).copy(floorMs = 6_000))
        t = 500
        assertTrue(c.estimate().secondsLeft >= 6L - 1)
    }

    @Test fun aFailureIsNotPadded() {
        val learned = o.associate { it.name to 100L }
        val failed = UnlockSnapshot.from(mapOf(UnlockStage.RESOLVE to StageState.ERRORED))
        val with = UnlockClock(learned, now = { t }).apply { observe(failed.copy(floorMs = 9_000)) }
        val without = UnlockClock(learned, now = { t }).apply { observe(failed) }
        t = 100
        assertEquals("the floor adds nothing to a failure", without.estimate().secondsLeft, with.estimate().secondsLeft)
        assertTrue(with.estimate().secondsLeft < 9L)
    }

    @Test fun aSkippedModelIsLearnedAtOnce() {
        // a phone that learned a 20 s model from an older box; this box skips MODEL at unlock
        val c = UnlockClock(mapOf(UnlockStage.MODEL.name to 20_000L), now = { t })
        c.observe(snap(emptySet(), UnlockStage.RESOLVE))
        t = 4_000; c.observe(snap(upTo(UnlockStage.DAEMONS), UnlockStage.DAEMONS))
        t = 5_000
        val states = HashMap<UnlockStage, StageState>()
        o.forEach { states[it] = StageState.COMPLETE }
        states[UnlockStage.MODEL] = StageState.SKIPPED
        c.observe(UnlockSnapshot.from(states))
        val learned = c.learn()!!
        assertEquals("not averaged with the 20 s it used to take", 100L, learned[UnlockStage.MODEL.name])
        // the next unlock does not add 20 s for a step the box no longer does
        val next = UnlockClock(learned, now = { t })
        next.observe(snap(upTo(UnlockStage.DAEMONS), UnlockStage.DAEMONS))
        assertTrue(next.estimate().secondsLeft <= 3L)
    }
}
