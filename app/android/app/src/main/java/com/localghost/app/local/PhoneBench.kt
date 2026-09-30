package com.localghost.app.local

import java.util.Locale

/**
 * THE PHONE MODEL'S BENCHMARK: how fast this phone loads the model, reads a prompt and writes an
 * answer, measured by the runtime itself (llama.cpp's own token counts and timings, not a stopwatch
 * around the app). Two fixed tests so runs compare across days and models: a long passage to read
 * (prefill, the cost of web notes and history) and a short question to answer at length (generation,
 * the words appearing). What a person feels comes out of those two numbers: how long before the
 * first word of a typical answer, and how long the whole of it takes.
 *
 * This file is pure (the record, the words, the history's storage format); [LocalModel.benchmark]
 * runs it.
 */
object PhoneBench {
    /** One run. Times in ms, as the runtime counted them. */
    data class Run(
        val at: Long,             // wall clock, ms
        val model: String,        // the GGUF's file name
        val loadMs: Long,         // the load this run did, or the last load when already in memory
        val loadedNow: Boolean,   // true when this run loaded the model itself
        val readTokens: Int, val readMs: Long,
        val writeTokens: Int, val writeMs: Long,
        val threads: Int,
        val device: String,       // "Samsung SM-S936B · SM8750 · 8 cores"
    ) {
        val readTps: Double get() = if (readMs > 0) readTokens * 1000.0 / readMs else 0.0
        val writeTps: Double get() = if (writeMs > 0) writeTokens * 1000.0 / writeMs else 0.0

        /** Seconds for an answer of [answerTokens] after a prompt of [promptTokens]. */
        fun seconds(promptTokens: Int, answerTokens: Int): Double =
            (if (readTps > 0) promptTokens / readTps else 0.0) + (if (writeTps > 0) answerTokens / writeTps else 0.0)
    }

    /** The reading test: a plain passage of about 450 tokens, then a one-word reply. */
    val READ_PROMPT: String = buildString {
        for (i in 1..24) append("Note $i: the harbour log for day $i lists ${i * 3} boats in, ${i * 2} out, wind from the ${listOf("north", "east", "south", "west")[i % 4]}, and a calm evening.\n")
        append("\nReply with the single word OK.")
    }
    const val READ_MAX_TOKENS = 4

    /** The writing test: a short question, a long answer (up to [WRITE_MAX_TOKENS]). */
    const val WRITE_PROMPT = "Describe, in one long paragraph, how a lighthouse keeper's day went before automation."
    const val WRITE_MAX_TOKENS = 160

    /** A typical chat turn, for the "what it feels like" lines: a question with some history. */
    const val TYPICAL_PROMPT = 250
    const val TYPICAL_ANSWER = 200

    fun tps(v: Double): String = if (v >= 50) String.format(Locale.US, "%.0f", v) else String.format(Locale.US, "%.1f", v)
    fun secs(v: Double): String = if (v >= 10) String.format(Locale.US, "%.0f s", v) else String.format(Locale.US, "%.1f s", v)

    /** The lines the MODELS screen shows for a run. */
    fun lines(r: Run): List<Pair<String, String>> = listOf(
        "loads in" to (secs(r.loadMs / 1000.0) + if (r.loadedNow) "" else " (last load)"),
        "reads" to "${tps(r.readTps)} tokens/s (${r.readTokens} tokens in ${secs(r.readMs / 1000.0)})",
        "writes" to "${tps(r.writeTps)} tokens/s (${r.writeTokens} tokens in ${secs(r.writeMs / 1000.0)})",
        "first word" to "after about ${secs(r.seconds(TYPICAL_PROMPT, 1))} for a ${TYPICAL_PROMPT}-token question",
        "a whole answer" to "about ${secs(r.seconds(TYPICAL_PROMPT, TYPICAL_ANSWER))} for ${TYPICAL_ANSWER} tokens (~150 words)",
        "on" to "${r.device} · ${r.threads} threads",
    )

    /** One line per past run, newest first: "30 Sep 14:02 · reads 85 · writes 12.3 · gemma…". */
    fun short(r: Run): String {
        val t = java.text.SimpleDateFormat("d MMM HH:mm", Locale.UK).format(java.util.Date(r.at))
        return "$t · reads ${tps(r.readTps)} · writes ${tps(r.writeTps)} tok/s · ${r.model.removeSuffix(".gguf")}"
    }

    // --- storage: the last runs, newest first, as JSON in preferences ---

    const val KEEP = 8

    fun toJson(runs: List<Run>): String = org.json.JSONObject().put("runs", org.json.JSONArray().apply {
        runs.take(KEEP).forEach { r ->
            put(org.json.JSONObject().put("at", r.at).put("model", r.model).put("loadMs", r.loadMs).put("loadedNow", r.loadedNow)
                .put("rt", r.readTokens).put("rms", r.readMs).put("wt", r.writeTokens).put("wms", r.writeMs)
                .put("threads", r.threads).put("device", r.device))
        }
    }).toString()

    fun fromJson(s: String?): List<Run> = try {
        val a = org.json.JSONObject(s ?: "{}").optJSONArray("runs") ?: return emptyList()
        (0 until a.length()).mapNotNull { i ->
            val o = a.optJSONObject(i) ?: return@mapNotNull null
            Run(o.optLong("at"), o.optString("model"), o.optLong("loadMs"), o.optBoolean("loadedNow"),
                o.optInt("rt"), o.optLong("rms"), o.optInt("wt"), o.optLong("wms"), o.optInt("threads"), o.optString("device"))
        }
    } catch (_: Exception) { emptyList() }
}

/** tokens per second from a count and milliseconds, as the chat's line under a phone answer shows it. */
object PhoneBenchWords {
    fun tps(tokens: Int, ms: Long): String = PhoneBench.tps(if (ms > 0) tokens * 1000.0 / ms else 0.0)
}
