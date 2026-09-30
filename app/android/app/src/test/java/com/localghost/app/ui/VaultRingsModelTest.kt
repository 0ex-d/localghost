package com.localghost.app.ui

import com.localghost.app.net.StageState
import com.localghost.app.net.UnlockSnapshot
import com.localghost.app.net.UnlockStage
import com.localghost.app.ui.VaultRingsModel.Phase
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class VaultRingsModelTest {
    @Test fun anUnlockLightsTheRingsOutsideIn() {
        val s = UnlockSnapshot.from(mapOf(
            UnlockStage.RESOLVE to StageState.COMPLETE, UnlockStage.UNSEAL to StageState.COMPLETE,
            UnlockStage.MOUNT to StageState.RUNNING))
        val p = VaultRingsModel.phases(s, locking = false)
        assertEquals(listOf(Phase.LIT, Phase.LIT, Phase.FILLING, Phase.DARK, Phase.DARK, Phase.DARK), p)
        assertEquals(2, VaultRingsModel.settled(p, locking = false))
    }

    @Test fun theModelHasNoRingAndReadyIsTheArrival() {
        // the box skips MODEL at unlock: every ring is lit once the services are up
        val all = UnlockStage.order.associateWith { StageState.COMPLETE }.toMutableMap()
        all[UnlockStage.MODEL] = StageState.SKIPPED
        val p = VaultRingsModel.phases(UnlockSnapshot.from(all), locking = false)
        assertTrue(p.all { it == Phase.LIT })
        assertEquals(6, VaultRingsModel.RINGS.size)
    }

    @Test fun aFailedStepTurnsItsRingRed() {
        val s = UnlockSnapshot.from(mapOf(UnlockStage.RESOLVE to StageState.ERRORED))
        assertEquals(Phase.ERROR, VaultRingsModel.phases(s, locking = false)[0])
    }

    @Test fun aLockPutsTheRingsOutInsideOut() {
        val start = VaultRingsModel.phases(UnlockSnapshot.teardown(emptyMap()), locking = true)
        assertTrue("a lock starts with every ring lit", start.all { it == Phase.LIT })
        val mid = VaultRingsModel.phases(UnlockSnapshot.teardown(mapOf(
            UnlockStage.STOP_SERVICES to StageState.COMPLETE, UnlockStage.STOP_CACHE to StageState.RUNNING)), locking = true)
        assertEquals(listOf(Phase.LIT, Phase.LIT, Phase.LIT, Phase.LIT, Phase.DRAINING, Phase.DARK), mid)
        // unmounting puts out the store and the key together; LOCKED the last ring
        val unmounted = VaultRingsModel.phases(UnlockSnapshot.teardown(mapOf(
            UnlockStage.STOP_SERVICES to StageState.COMPLETE, UnlockStage.STOP_CACHE to StageState.COMPLETE,
            UnlockStage.STOP_DB to StageState.COMPLETE, UnlockStage.UNMOUNT to StageState.COMPLETE)), locking = true)
        assertEquals(listOf(Phase.LIT, Phase.DARK, Phase.DARK, Phase.DARK, Phase.DARK, Phase.DARK), unmounted)
        val locked = VaultRingsModel.phases(UnlockSnapshot.teardown(UnlockStage.teardownOrder.associateWith { StageState.COMPLETE }), locking = true)
        assertTrue(locked.all { it == Phase.DARK })
        assertEquals(6, VaultRingsModel.settled(locked, locking = true))
    }

    @Test fun aRingLocksInWithItsKeywayAtTheTop() {
        for (from in listOf(-400f, -95f, -90f, 0f, 45f, 179f, 270f, 1234f)) {
            for (cw in listOf(true, false)) {
                val to = VaultRingsModel.restAngle(from, cw)
                val norm = ((to % 360f) + 360f) % 360f
                assertEquals("from $from", 270f, norm, 0.001f) // -90° is 270°
                if (cw) assertTrue("clockwise moves forward from $from", to > from)
                else assertTrue("anticlockwise moves back from $from", to < from)
                assertTrue("never more than one turn and a bit from $from", kotlin.math.abs(to - from) < 380f)
            }
        }
    }

    @Test fun ringsStepInwardsAndThinOut() {
        assertEquals(1f, VaultRingsModel.radiusShare(0), 0.0001f)
        assertEquals(0.40f, VaultRingsModel.radiusShare(5), 0.0001f)
        assertTrue((0 until 5).all { VaultRingsModel.radiusShare(it) > VaultRingsModel.radiusShare(it + 1) })
        assertTrue((0 until 5).all { VaultRingsModel.segments(it) > VaultRingsModel.segments(it + 1) })
    }
}
