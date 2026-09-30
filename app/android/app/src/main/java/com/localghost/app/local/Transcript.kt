package com.localghost.app.local

/**
 * The conversation so far, written in front of a question for the phone's model. Its runtime takes
 * one system and one user message (every call starts from an empty context), so the earlier turns
 * travel as text: the last few, newest kept, each clipped, all bounded to what a 4k context can
 * spare beside web notes and the answer. Pure; the words are tested.
 */
object Transcript {
    const val MAX_TURNS = 6
    const val MAX_TURN_CHARS = 600
    const val MAX_CHARS = 2400

    /** [history]: (fromPerson, text), oldest first, WITHOUT the current question. */
    fun withHistory(prompt: String, history: List<Pair<Boolean, String>>): String {
        val turns = history.filter { it.second.isNotBlank() }.takeLast(MAX_TURNS)
        if (turns.isEmpty()) return prompt
        val lines = ArrayList<String>()
        var used = 0
        for ((fromPerson, text) in turns.asReversed()) { // newest first, so the budget keeps the latest
            val t = text.replace(Regex("\\s+"), " ").trim().let { if (it.length > MAX_TURN_CHARS) it.take(MAX_TURN_CHARS) + "…" else it }
            val line = (if (fromPerson) "You: " else "Ghost: ") + t
            if (used + line.length > MAX_CHARS) break
            lines.add(line); used += line.length
        }
        if (lines.isEmpty()) return prompt
        return "The conversation so far, oldest first:\n" + lines.asReversed().joinToString("\n") +
            "\n\nThe next message, to answer as part of that conversation:\n" + prompt
    }
}
