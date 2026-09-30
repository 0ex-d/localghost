package com.localghost.app.net

/**
 * The unlock progress model, mirroring ghost.secd's stream. After a PIN is submitted the app polls
 * the box once a second for unlock state. The box returns the stages completed so far:
 *
 *  - HOT account (already mounted): every stage comes back done on the first poll , the screen fills
 *    in instantly and we proceed (skip, skip, skip).
 *  - COLD account: the box returns the stages it has finished so far; each poll adds more until it
 *    reaches Ready. The screen shows the whole list, ticking stages off as they complete.
 *
 * Critically, the stage set and order are IDENTICAL for every account , a real unlock and a duress
 * unlock stream the same stages with the same labels. The app cannot tell them apart, and must not
 * try. The only difference is how fast they fill in, which reflects warmth (how recently the box was
 * used), not which account opened.
 */
enum class UnlockStage(val label: String) {
    RESOLVE("checking"),
    UNSEAL("unsealing key"),
    MOUNT("mounting store"),
    START_DB("starting database"),
    START_CACHE("starting cache"),
    DAEMONS("starting services"),
    MODEL("loading model"),
    READY("ready"),

    // Teardown stages, mirroring the mount in reverse. Shown on lock so the spin-down is visible and
    // the next unlock is obviously a fresh cold start.
    STOP_SERVICES("stopping services"),
    STOP_CACHE("stopping cache"),
    STOP_DB("stopping database"),
    UNMOUNT("unmounting store"),
    LOCKED("locked");

    companion object {
        /**
         * The fixed order every unlock walks. Same for all accounts. Since 30 Sep 2026 the box
         * marks MODEL skipped and loads the model after READY, off the unlock's path; chat shows
         * that load while it lasts ([BoxClient.modelStatus]). A box from before still runs MODEL
         * here and sends its load's own estimate ([ModelLoad]).
         */
        val order = listOf(RESOLVE, UNSEAL, MOUNT, START_DB, START_CACHE, DAEMONS, MODEL, READY)

        /** The teardown order a lock walks , the mount in reverse. */
        val teardownOrder = listOf(STOP_SERVICES, STOP_CACHE, STOP_DB, UNMOUNT, LOCKED)

        /** Map a box stage name to the enum (both unlock and teardown names). */
        fun fromName(n: String): UnlockStage? = when (n) {
            "RESOLVE" -> RESOLVE
            "UNSEAL" -> UNSEAL
            "MOUNT" -> MOUNT
            "START_DB" -> START_DB
            "START_CACHE" -> START_CACHE
            "DAEMONS" -> DAEMONS
            "MODEL" -> MODEL
            "READY" -> READY
            "STOP_SERVICES" -> STOP_SERVICES
            "STOP_CACHE" -> STOP_CACHE
            "STOP_DB" -> STOP_DB
            "UNMOUNT" -> UNMOUNT
            "LOCKED" -> LOCKED
            else -> null
        }
    }
}

enum class StageState { PENDING, RUNNING, SKIPPED, COMPLETE, ERRORED }

/**
 * The model's load as the box measures it: oracled reads llama.cpp's own progress and knows how long
 * the last load took, and secd carries that on the unlock poll as {phase, pct, etaMs, elapsedMs}.
 * phase: starting, weights, projector, warmup, finishing, ready, failed. etaMs is -1 when unknown.
 */
data class ModelLoad(val phase: String, val pct: Int, val etaMs: Long, val elapsedMs: Long) {
    companion object {
        /** From the poll's "model" object; null when absent or empty (a box from before 29 Sep 2026). */
        fun fromJson(o: org.json.JSONObject?): ModelLoad? {
            if (o == null) return null
            val phase = o.optString("phase", "")
            if (phase.isEmpty()) return null
            return ModelLoad(phase, o.optInt("pct", 0).coerceIn(0, 100), o.optLong("etaMs", -1), o.optLong("elapsedMs", 0))
        }
    }
}

/** One stage's state in the current unlock, for rendering a row. */
data class StageProgress(val stage: UnlockStage, val state: StageState)

/**
 * A poll snapshot from the box: the state of every stage right now. Stages not yet reached are
 * PENDING. done is true once the account is open (READY complete), failed carries an error.
 */
data class UnlockSnapshot(
    val stages: List<StageProgress>,
    val done: Boolean,
    val failed: String? = null,
    /** The model's load while MODEL runs (oracled measures it; the poll carries it), else null. */
    val model: ModelLoad? = null,
    /** The shortest this unlock may LOOK (ms from the PIN). 0: no floor, the default since the box
     *  replays a cold unlock itself on a warm one (secd, replay.go). */
    val floorMs: Long = 0,
) {
    companion object {
        /** A full snapshot from a flat map of stage -> state, filling unreached stages as PENDING. */
        fun from(states: Map<UnlockStage, StageState>, model: ModelLoad? = null): UnlockSnapshot {
            val rows = UnlockStage.order.map { st ->
                StageProgress(st, states[st] ?: StageState.PENDING)
            }
            val ready = states[UnlockStage.READY] == StageState.COMPLETE
            val err = rows.firstOrNull { it.state == StageState.ERRORED }
            return UnlockSnapshot(
                stages = rows,
                done = ready,
                failed = if (err != null) "failed at ${err.stage.label}" else null,
                model = model,
            )
        }

        /** The initial snapshot shown the instant a PIN is submitted, before the first poll. */
        fun initial(): UnlockSnapshot = from(mapOf(UnlockStage.RESOLVE to StageState.RUNNING))

        /** A teardown snapshot from a flat map over the LOCK stages (used by the lock spin-down view). */
        fun teardown(states: Map<UnlockStage, StageState>): UnlockSnapshot {
            val rows = UnlockStage.teardownOrder.map { st ->
                StageProgress(st, states[st] ?: StageState.PENDING)
            }
            val done = states[UnlockStage.LOCKED] == StageState.COMPLETE
            val err = rows.firstOrNull { it.state == StageState.ERRORED }
            return UnlockSnapshot(
                stages = rows,
                done = done,
                failed = if (err != null) "failed at ${err.stage.label}" else null,
            )
        }

        /** A terminal snapshot for a transport failure (could not reach or lost contact with box). */
        fun failed(reason: String): UnlockSnapshot =
            UnlockSnapshot(
                stages = UnlockStage.order.map { StageProgress(it, StageState.PENDING) },
                done = false,
                failed = reason,
            )
    }
}
