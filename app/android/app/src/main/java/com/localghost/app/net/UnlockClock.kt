package com.localghost.app.net

/**
 * Where an unlock (or a lock) is in TIME, and how long each step takes on this box, learned.
 *
 * The box streams stages; it does not say how long they take. The phone times them itself, on its
 * own clock, from the snapshots it sees: a stage's time is from the moment the previous one was seen
 * done to the moment this one is seen done (the poll is once a second, so this is what the person
 * actually waits). After a cold unlock that finished, each stage's time is folded into what this
 * phone expects of that stage next time. Until then, [DEFAULTS]. While MODEL runs, the box's own
 * estimate wins: oracled measures the load and knows the last one's length.
 *
 * The bar is time: elapsed / (elapsed + what is left). It never goes back, and it holds short of
 * the end until the box says ready. The same numbers come out for any account (they are built only
 * from the stage stream, which is identical for every account), so a duress unlock looks the same.
 *
 * THE FLOOR ([UnlockSnapshot.floorMs], 0 unless a caller sets one). A box that was already open
 * would answer "ready" in a second and say it had been unlocked before. Since 30 Sep 2026 the box
 * hides that itself: a warm unlock replays one of its own last eight cold ones, step by step (secd,
 * replay.go), so the screen sets no floor. The padding below stays for a caller that wants one:
 * until the floor has passed, a finished unlock is walked through in order (never back past a step
 * already shown) and the time left counts down to it. Learning uses the real times.
 *
 * MODEL: a box from 30 Sep 2026 skips it at unlock and loads the model afterwards (chat shows that
 * load, [com.localghost.app.net.BoxClient.modelStatus]). A skipped stage is learned as next to
 * nothing at once, not averaged down over several unlocks.
 *
 * Pure: the screen feeds snapshots and asks [estimate] on its own tick; tests drive [now].
 */
class UnlockClock(
    learned: Map<String, Long> = emptyMap(),
    private val now: () -> Long = System::currentTimeMillis,
) {
    private val expect = HashMap(learned)
    private var startedAt = -1L
    private val doneAt = LinkedHashMap<UnlockStage, Long>()
    private var last: UnlockSnapshot? = null
    private var model: ModelLoad? = null
    private var modelAt = 0L
    private var shown = 0f
    private var warm = false
    private var learnedThisRun = false
    private var padIdx = -1 // the furthest step shown while padding to the floor

    /** Feeds one snapshot (the stream's, as it arrives). */
    fun observe(s: UnlockSnapshot) {
        val t = now()
        if (startedAt < 0) startedAt = t
        // from nothing done to everything done in one look: a warm box, nothing to time or learn
        if (doneAt.isEmpty() && s.done) warm = true
        for (row in s.stages) {
            if ((row.state == StageState.COMPLETE || row.state == StageState.SKIPPED) && row.stage !in doneAt) {
                doneAt[row.stage] = t
            }
        }
        if (s.model != null && s.model != model) { model = s.model; modelAt = t }
        // the furthest step the real stream has shown: padding to the floor never goes back past it
        if (!s.done) {
            val cur = s.stages.indexOfFirst { it.state != StageState.COMPLETE && it.state != StageState.SKIPPED }
            if (cur >= 0) padIdx = maxOf(padIdx, cur)
        }
        last = s
    }

    /** What the screen shows now. */
    fun estimate(): UnlockEstimate {
        val s = last ?: return UnlockEstimate(0f, -1, null, emptyMap(), confident = false, warm = false, overdue = false)
        val t = now()
        val order = s.stages.map { it.stage }
        val took = LinkedHashMap<UnlockStage, Long>()
        var prev = startedAt
        var current: UnlockStage? = null
        for (st in order) {
            val d = doneAt[st]
            if (d != null) { took[st] = (d - prev).coerceAtLeast(0); prev = d; continue }
            current = st
            break
        }
        val elapsed0 = (t - startedAt).coerceAtLeast(0)
        if ((s.done || current == null) && s.failed == null && elapsed0 < s.floorMs) {
            return padded(order, took, elapsed0, s.floorMs)
        }
        if (s.done || current == null) {
            shown = 1f
            return UnlockEstimate(1f, 0, null, took, confident = true, warm = warm, overdue = false)
        }
        padIdx = maxOf(padIdx, order.indexOf(current))
        val inCurrent = (t - prev).coerceAtLeast(0)
        took[current] = inCurrent
        val m = model
        val boxSays = current == UnlockStage.MODEL && m != null && m.etaMs >= 0 && m.phase != "ready" && m.phase != "failed"
        val currentLeft = if (boxSays) {
            (m.etaMs - (t - modelAt)).coerceAtLeast(500)
        } else {
            val e = expected(current)
            // over its usual time: a little more, never "done" before the box says so
            (e - inCurrent).coerceAtLeast(maxOf(1_000L, e / 6))
        }
        val after = order.dropWhile { it != current }.drop(1).sumOf { expected(it) }
        val elapsed = elapsed0
        val left = if (s.failed == null) maxOf(currentLeft + after, s.floorMs - elapsed) // never "sooner" than the floor
            else currentLeft + after
        val raw = if (elapsed + left > 0) elapsed.toFloat() / (elapsed + left) else 0f
        shown = maxOf(shown, raw.coerceAtMost(0.98f))
        val confident = boxSays || order.any { it.name in expect }
        // well past what this stage usually takes, and the box gives no estimate of its own
        val overdue = !boxSays && inCurrent > 5_000 && inCurrent > 2 * expected(current)
        return UnlockEstimate(shown, (left + 999) / 1000, current, took, confident, warm, overdue)
    }

    /**
     * Done, but short of the floor: the steps are walked in order so the finish lands on the floor.
     * The step for this moment is the share of the floor gone by, never one before the furthest the
     * real stream (or this walk) already showed; its time is counted from when the walk reached it.
     */
    private fun padded(order: List<UnlockStage>, real: Map<UnlockStage, Long>, elapsed: Long, floor: Long): UnlockEstimate {
        val steps = order.dropLast(1) // the last (ready) is the arrival, not a step
        if (steps.isEmpty()) return UnlockEstimate(1f, 0, null, real, confident = true, warm = warm, overdue = false)
        val per = floor.toDouble() / steps.size
        val byTime = (elapsed / per).toInt().coerceIn(0, steps.size - 1)
        val idx = maxOf(byTime, padIdx).coerceIn(0, steps.size - 1)
        padIdx = idx
        val cur = steps[idx]
        val took = LinkedHashMap<UnlockStage, Long>()
        for (st in steps.take(idx)) took[st] = real[st] ?: per.toLong()
        took[cur] = (elapsed - (idx * per).toLong()).coerceAtLeast(0)
        shown = maxOf(shown, (elapsed.toFloat() / floor).coerceAtMost(0.98f))
        val left = ((floor - elapsed) + 999) / 1000
        return UnlockEstimate(shown, left, cur, took, confident = true, warm = warm, overdue = false)
    }

    /**
     * After a cold run that finished: each stage's time folded into what is expected of it (the
     * old and the new averaged, so one odd unlock moves it halfway). Returns the map to store, or
     * null when there is nothing to learn (warm, failed, not done, or already learned).
     */
    fun learn(): Map<String, Long>? {
        val s = last ?: return null
        if (!s.done || s.failed != null || warm || learnedThisRun) return null
        learnedThisRun = true
        var prev = startedAt
        for (row in s.stages) {
            val st = row.stage
            val d = doneAt[st] ?: break
            val took = (d - prev).coerceIn(100, 10 * 60_000L)
            prev = d
            // skipped: the box no longer does it at all, so no average with what it used to take
            expect[st.name] = if (row.state == StageState.SKIPPED) 100
                else expect[st.name]?.let { (it + took) / 2 } ?: took
        }
        return HashMap(expect)
    }

    private fun expected(st: UnlockStage): Long = expect[st.name] ?: DEFAULTS[st] ?: 1_000

    companion object {
        /** A first unlock's guess, before this phone has timed one (ms). */
        val DEFAULTS: Map<UnlockStage, Long> = mapOf(
            UnlockStage.RESOLVE to 1_000, UnlockStage.UNSEAL to 2_000, UnlockStage.MOUNT to 2_000,
            UnlockStage.START_DB to 3_000, UnlockStage.START_CACHE to 3_000, UnlockStage.DAEMONS to 1_000,
            UnlockStage.MODEL to 500, // skipped at unlock since 30 Sep 2026 (a live load sends its own eta)
             UnlockStage.READY to 500,
            UnlockStage.STOP_SERVICES to 3_000, UnlockStage.STOP_CACHE to 1_000, UnlockStage.STOP_DB to 3_000,
            UnlockStage.UNMOUNT to 2_000, UnlockStage.LOCKED to 500,
        )
    }
}

/**
 * [fraction] 0..1 for the bar; [secondsLeft] -1 before the first snapshot; [current] the stage
 * running now (null when done); [took] each stage's time so far (the running one's live);
 * [confident] false on a phone that has never timed an unlock and has no word from the box yet;
 * [overdue] the running stage has taken more than twice its usual time (and at least 5 s).
 */
data class UnlockEstimate(
    val fraction: Float,
    val secondsLeft: Long,
    val current: UnlockStage?,
    val took: Map<UnlockStage, Long>,
    val confident: Boolean,
    val warm: Boolean,
    val overdue: Boolean,
)
