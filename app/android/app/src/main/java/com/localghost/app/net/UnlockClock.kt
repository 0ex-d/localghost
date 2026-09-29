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
        if (s.done || current == null) {
            shown = 1f
            return UnlockEstimate(1f, 0, null, took, confident = true, warm = warm, overdue = false)
        }
        val inCurrent = (t - prev).coerceAtLeast(0)
        took[current] = inCurrent
        val m = model
        val boxSays = current == UnlockStage.MODEL && m != null && m.etaMs >= 0 && m.phase != "ready" && m.phase != "failed"
        val currentLeft = if (boxSays) {
            (m!!.etaMs - (t - modelAt)).coerceAtLeast(500)
        } else {
            val e = expected(current)
            // over its usual time: a little more, never "done" before the box says so
            (e - inCurrent).coerceAtLeast(maxOf(1_000L, e / 6))
        }
        val after = order.dropWhile { it != current }.drop(1).sumOf { expected(it) }
        val left = currentLeft + after
        val elapsed = (t - startedAt).coerceAtLeast(0)
        val raw = if (elapsed + left > 0) elapsed.toFloat() / (elapsed + left) else 0f
        shown = maxOf(shown, raw.coerceAtMost(0.98f))
        val confident = boxSays || order.any { it.name in expect }
        // well past what this stage usually takes, and the box gives no estimate of its own
        val overdue = !boxSays && inCurrent > 5_000 && inCurrent > 2 * expected(current)
        return UnlockEstimate(shown, (left + 999) / 1000, current, took, confident, warm, overdue)
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
        for (st in s.stages.map { it.stage }) {
            val d = doneAt[st] ?: break
            val took = (d - prev).coerceIn(100, 10 * 60_000L)
            prev = d
            expect[st.name] = expect[st.name]?.let { (it + took) / 2 } ?: took
        }
        return HashMap(expect)
    }

    private fun expected(st: UnlockStage): Long = expect[st.name] ?: DEFAULTS[st] ?: 1_000

    companion object {
        /** A first unlock's guess, before this phone has timed one (ms). */
        val DEFAULTS: Map<UnlockStage, Long> = mapOf(
            UnlockStage.RESOLVE to 1_000, UnlockStage.UNSEAL to 2_000, UnlockStage.MOUNT to 2_000,
            UnlockStage.START_DB to 3_000, UnlockStage.START_CACHE to 3_000, UnlockStage.DAEMONS to 1_000,
            UnlockStage.MODEL to 30_000, UnlockStage.READY to 500,
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
