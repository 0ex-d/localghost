package com.localghost.app.net

/**
 * Slows the unlock down enough to WATCH. The box streams stages as fast as they happen, and since
 * 30 Sep 2026 a warm box replays one of its own cold unlocks (secd, replay.go), so all six steps
 * can arrive in a single poll and the vault rings light at once, faster than anyone can read. The
 * pacer holds each step on screen for at least [minStepMs] and lets the steps advance in order, so
 * the animation is always legible; a step that genuinely takes longer keeps its real length (the
 * pacer never shows it done before the box did). It never runs ahead of the box and never goes back.
 *
 * It does not invent progress: it only DELAYS showing a step done, so a duress unlock and a real one
 * still look the same (both are paced the same way from the same stream). A failure stops the pacing
 * at once , the real snapshot is shown so the person sees where it stopped.
 *
 * Pure: feed it [observe] snapshots as they arrive and ask [display] on a fast tick; tests drive
 * [now]. Works for a lock too (the teardown stages), by the same rule.
 */
class UnlockPacer(
    private val minStepMs: Long = MIN_STEP_MS,
    private val now: () -> Long = System::currentTimeMillis,
) {
    private var startedAt = -1L
    private val realDoneAt = LinkedHashMap<UnlockStage, Long>() // when the box first showed the step done
    private var last: UnlockSnapshot? = null

    /** Feeds one real snapshot from the box. */
    fun observe(s: UnlockSnapshot) {
        val t = now()
        if (startedAt < 0) startedAt = t
        for (row in s.stages) {
            if ((row.state == StageState.COMPLETE || row.state == StageState.SKIPPED) && row.stage !in realDoneAt) {
                realDoneAt[row.stage] = t
            }
        }
        last = s
    }

    /**
     * The snapshot to show now: the box's, with each step held for at least [minStepMs] and the
     * steps walked in order. Null before the first [observe]. On a failure the real snapshot passes
     * through unchanged.
     */
    fun display(): UnlockSnapshot? {
        val s = last ?: return null
        if (s.failed != null || s.stages.any { it.state == StageState.ERRORED }) return s
        val t = now()
        // paced done-time per step, in stream order: no earlier than the box, and at least one
        // min-step after the step before it. Stop at the first step the box has not finished, so a
        // step still running on the box is never shown done. A SKIPPED step (MODEL, off the unlock's
        // path) and the final arrival step (READY / LOCKED, which the iris and the switch-off stand
        // in for) add no minimum , only the real steps are held.
        val shownDoneAt = HashMap<UnlockStage, Long>()
        var prevShown = startedAt
        val lastStage = s.stages.lastOrNull()?.stage
        for (row in s.stages) {
            val rd = realDoneAt[row.stage] ?: break
            // held only if it has a ring: not skipped, not MODEL (off the unlock's path, no ring),
            // and not the final arrival step (READY / LOCKED, which the iris and switch-off cover)
            val holds = row.state != StageState.SKIPPED && row.stage != UnlockStage.MODEL && row.stage != lastStage
            val sd = if (holds) maxOf(rd, prevShown + minStepMs) else maxOf(rd, prevShown)
            shownDoneAt[row.stage] = sd
            prevShown = sd
        }
        val stages = s.stages.map { row ->
            val sd = shownDoneAt[row.stage]
            when {
                sd != null && t >= sd ->
                    StageProgress(row.stage, if (row.state == StageState.SKIPPED) StageState.SKIPPED else StageState.COMPLETE)
                else -> StageProgress(row.stage, StageState.PENDING)
            }
        }.toMutableList()
        // the running step: the first not-yet-shown-done one (its predecessors are all shown done)
        for (i in stages.indices) {
            if (stages[i].state == StageState.PENDING) {
                stages[i] = StageProgress(stages[i].stage, StageState.RUNNING)
                break
            }
        }
        val allShown = stages.all { it.state == StageState.COMPLETE || it.state == StageState.SKIPPED }
        return s.copy(stages = stages, done = s.done && allShown)
    }

    companion object {
        /** How long each step is held on screen at least, so the ring for it can fill and lock in
         *  (the ring's own lock-in is ~420 ms; a bit more lets the fill show first). Six steps make
         *  a floor of about four seconds, plus the iris. */
        const val MIN_STEP_MS = 720L

        /** The ring-bearing unlock steps (RESOLVE, UNSEAL, MOUNT, START_DB, START_CACHE, DAEMONS);
         *  MODEL is skipped and READY is the arrival, so neither is held. */
        const val RING_STEPS = 6

        /** The shortest an unlock is shown for, so every ring is readable even on a warm box: the
         *  ring steps, each held [MIN_STEP_MS]. The caller waits this out before opening the iris. */
        const val FLOOR_MS = RING_STEPS * MIN_STEP_MS
    }
}
