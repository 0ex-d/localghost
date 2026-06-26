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
    READY("ready");

    companion object {
        /** The fixed order every unlock walks. Same for all accounts. */
        val order = listOf(RESOLVE, UNSEAL, MOUNT, START_DB, START_CACHE, DAEMONS, READY)
    }
}

enum class StageState { PENDING, RUNNING, SKIPPED, COMPLETE, ERRORED }

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
) {
    companion object {
        /** A full snapshot from a flat map of stage -> state, filling unreached stages as PENDING. */
        fun from(states: Map<UnlockStage, StageState>): UnlockSnapshot {
            val rows = UnlockStage.order.map { st ->
                StageProgress(st, states[st] ?: StageState.PENDING)
            }
            val ready = states[UnlockStage.READY] == StageState.COMPLETE
            val err = rows.firstOrNull { it.state == StageState.ERRORED }
            return UnlockSnapshot(
                stages = rows,
                done = ready,
                failed = if (err != null) "failed at ${err.stage.label}" else null,
            )
        }

        /** The initial snapshot shown the instant a PIN is submitted, before the first poll. */
        fun initial(): UnlockSnapshot = from(mapOf(UnlockStage.RESOLVE to StageState.RUNNING))

        /** A terminal snapshot for a transport failure (could not reach or lost contact with box). */
        fun failed(reason: String): UnlockSnapshot =
            UnlockSnapshot(
                stages = UnlockStage.order.map { StageProgress(it, StageState.PENDING) },
                done = false,
                failed = reason,
            )
    }
}
