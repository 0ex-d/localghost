package com.localghost.app.net

/**
 * A short follow-up read with the question before it, for when the phone plans a web search by
 * itself (the box's model did not answer the plan in time, or said it was on the CPU). The box's
 * plan resolves "how about now?" from the conversation; without it the phone used to search the
 * three words as they stood and came back with a song. Now a follow-up borrows the previous
 * question and adds whatever the new one brings ("and in euros?" → the rate question + euros).
 * Pure; the words are tested.
 */
object FollowUp {
    // words that carry no topic of their own in a follow-up
    private val stop = setOf(
        "and", "but", "also", "so", "then", "ok", "okay", "what", "how", "about", "now", "today", "tonight",
        "currently", "current", "latest", "again", "it", "its", "it's", "that", "this", "these", "those", "there",
        "them", "they", "one", "ones", "same", "the", "a", "an", "of", "for", "in", "on", "at", "to", "is", "are",
        "was", "were", "be", "me", "my", "please", "pls", "if", "else", "else?", "more", "instead", "too", "any",
        "do", "does", "did", "you", "your", "we", "with", "right", "yet", "still", "here", "why", "when", "where",
        "which", "who", "can", "could", "would", "will", "tell", "show",
    )
    private val lead = Regex("^\\s*(and|but|also|what about|how about|what if|same|then|so|ok and|and now|and what about)\\b", RegexOption.IGNORE_CASE)

    /** The question to search: [question] itself, or, when it is a follow-up, the previous
     *  question with what the follow-up adds. [earlier] are the person's earlier questions,
     *  oldest first. */
    fun standalone(question: String, earlier: List<String>): String {
        val prev = earlier.lastOrNull { it.isNotBlank() }?.trim() ?: return question
        val words = question.lowercase().split(Regex("[^\\p{L}\\p{N}$€£'.-]+")).map { it.trim('.', '\'') }.filter { it.isNotEmpty() }
        val content = words.filter { it !in stop }
        val followUp = lead.containsMatchIn(question) || words.size <= 4 || content.isEmpty()
        if (!followUp || content.size >= 4) return question
        val base = prev.trimEnd('?', '.', '!', ' ')
        return if (content.isEmpty()) base else base + " " + content.joinToString(" ")
    }
}
