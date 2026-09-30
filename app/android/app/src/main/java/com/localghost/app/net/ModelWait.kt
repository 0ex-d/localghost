package com.localghost.app.net

import com.localghost.app.local.TransferRate

/**
 * Chat before the model is ready. Since 30 Sep 2026 the box loads its model after the unlock, off
 * the unlock's path, so a question asked in the first seconds after a cold unlock would otherwise
 * sit on "asking your box…" with nothing moving. Chat asks GET /v1/model first and, while the load
 * runs, shows it in the answer's place (what is loading, how far, how long is left), then sends
 * once the model answers. The phone's own model gets the same kind of line from its last load time.
 *
 * Pure: MainActivity polls and feeds; the tests drive it.
 */
object ModelWait {
    /** How long chat waits on the box's model before it asks anyway (the box's answer then says why). */
    const val BOX_WAIT_MS = 3 * 60_000L

    /** How often chat asks the box again while its model loads. */
    const val POLL_MS = 1_000L

    /** GET /v1/model: [ready] when the model answers; else how far its load is ([phase], [pct],
     *  [etaMs] -1 when the box cannot tell yet). */
    data class Box(val ready: Boolean, val phase: String, val pct: Int, val etaMs: Long, val detail: String) {
        companion object {
            /** Null when the answer is not a model state (an older box has no /v1/model). */
            fun fromJson(o: org.json.JSONObject?): Box? {
                if (o == null || !o.has("ready")) return null
                return Box(o.optBoolean("ready", false), o.optString("phase", ""),
                    o.optInt("pct", 0).coerceIn(0, 100), o.optLong("etaMs", -1), o.optString("detail", ""))
            }
        }
    }

    /** The status line while the box's model loads: what, how far, how long is left. */
    fun boxLine(m: Box): String {
        val what = when (m.phase) {
            "weights" -> "your box is loading its model into the GPU"
            "projector" -> "your box is loading the part of its model that sees photos"
            "warmup", "finishing" -> "your box is warming its model up with a first word"
            "failed" -> "your box's model did not load , the box is trying again"
            "ready" -> "your box's model is ready"
            else -> "your box is starting its model"
        }
        val pct = if (m.phase == "weights" && m.pct in 1..99) " · ${m.pct}%" else ""
        val left = if (m.etaMs >= 0 && m.phase != "failed") TransferRate.left((m.etaMs + 999) / 1000) else ""
        return what + pct + (if (left.isNotEmpty()) " · $left" else "") + " , your question goes as soon as it is ready"
    }

    /** Waited [BOX_WAIT_MS] and the model is still not up: chat asks anyway, and says so. */
    fun boxGaveUp(m: Box?): String =
        "your box's model is still not ready" + (m?.detail?.takeIf { it.isNotBlank() }?.let { " ($it)" } ?: "") + " , asking anyway…"

    /**
     * The phone's own model, loading: seconds so far and, when this phone has loaded it before,
     * the time the last load took ([lastLoadMs], 0 when never measured).
     */
    fun phoneLine(elapsedMs: Long, lastLoadMs: Long): String {
        val s = elapsedMs / 1000
        val base = "loading the phone's model"
        if (lastLoadMs <= 0) return if (s < 1) "$base…" else "$base · $s s…"
        val left = ((lastLoadMs - elapsedMs) + 999) / 1000
        return if (left > 0) "$base · ${TransferRate.left(left)}" else "$base · $s s, longer than last time…"
    }
}
