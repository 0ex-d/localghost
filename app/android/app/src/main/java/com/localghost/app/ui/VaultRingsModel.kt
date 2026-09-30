package com.localghost.app.ui

import com.localghost.app.net.StageState
import com.localghost.app.net.UnlockSnapshot
import com.localghost.app.net.UnlockStage

/**
 * What each of the vault rings shows for a snapshot of the unlock (or the lock). Pure, so the
 * tests read it; [VaultRings] draws it.
 *
 * One ring per step of the unlock, outside in: checking, unsealing, mounting, database, cache,
 * services. MODEL is off the unlock's path since 30 Sep 2026 and READY is the arrival, so neither
 * has a ring. A lock undoes the unlock from the inside out, each teardown step putting out the
 * ring of the step it undoes. Built from the stage stream alone, which the box sends identically
 * for every account.
 */
object VaultRingsModel {
    val RINGS: List<UnlockStage> = listOf(
        UnlockStage.RESOLVE, UnlockStage.UNSEAL, UnlockStage.MOUNT,
        UnlockStage.START_DB, UnlockStage.START_CACHE, UnlockStage.DAEMONS,
    )

    /** The teardown step that puts each ring out. Unmounting closes the key and the store. */
    private val PUT_OUT_BY: Map<UnlockStage, UnlockStage> = mapOf(
        UnlockStage.DAEMONS to UnlockStage.STOP_SERVICES,
        UnlockStage.START_CACHE to UnlockStage.STOP_CACHE,
        UnlockStage.START_DB to UnlockStage.STOP_DB,
        UnlockStage.MOUNT to UnlockStage.UNMOUNT,
        UnlockStage.UNSEAL to UnlockStage.UNMOUNT,
        UnlockStage.RESOLVE to UnlockStage.LOCKED,
    )

    /** DARK: not reached (or put out). FILLING: its step is running. LIT: done, locked in place.
     *  DRAINING: its teardown step is running. ERROR: its step failed. */
    enum class Phase { DARK, FILLING, LIT, DRAINING, ERROR }

    fun phases(s: UnlockSnapshot, locking: Boolean): List<Phase> {
        val state = s.stages.associate { it.stage to it.state }
        return RINGS.map { ring ->
            if (!locking) when (state[ring]) {
                StageState.COMPLETE, StageState.SKIPPED -> Phase.LIT
                StageState.RUNNING -> Phase.FILLING
                StageState.ERRORED -> Phase.ERROR
                else -> Phase.DARK
            } else when (state[PUT_OUT_BY.getValue(ring)]) {
                StageState.COMPLETE, StageState.SKIPPED -> Phase.DARK
                StageState.RUNNING -> Phase.DRAINING
                StageState.ERRORED -> Phase.ERROR
                else -> Phase.LIT
            }
        }
    }

    /** Rings settled so far: lit on an unlock, put out on a lock. A haptic tick follows it. */
    fun settled(phases: List<Phase>, locking: Boolean): Int =
        if (!locking) phases.count { it == Phase.LIT } else phases.count { it == Phase.DARK }

    /** The ring's radius as a share of the outer one: ring 0 is the outermost. */
    fun radiusShare(ring: Int, inner: Float = 0.40f): Float =
        1f - ring * (1f - inner) / (RINGS.size - 1)

    /** Segments on ring [ring]: fewer towards the centre, so they stay the same length on screen. */
    fun segments(ring: Int): Int = 36 - ring * 4

    /** Where a ring comes to rest when it locks in: the next angle, in its direction of travel,
     *  that puts its keyway at the top (-90°). [from] and the result are in degrees. */
    fun restAngle(from: Float, clockwise: Boolean): Float {
        val target = -90f
        var d = ((target - from) % 360f + 360f) % 360f // 0..360, clockwise distance
        if (!clockwise) d -= 360f
        if (clockwise && d < 20f) d += 360f   // never a snap back: at least a little travel
        if (!clockwise && d > -20f) d -= 360f
        return from + d
    }
}
