package com.localghost.app.ui

import java.util.Locale

/** One unreadable photo's line on Box Status: when it was taken, how big, why. Pure, for the tests. */
object UnreadableText {
    fun row(takenAt: Long, bytes: Long, why: String): String {
        val f = java.text.SimpleDateFormat("d MMM yyyy HH:mm", Locale.UK).apply { timeZone = java.util.TimeZone.getTimeZone("UTC") }
        val size = when {
            bytes >= 1_000_000 -> "%.1f MB".format(Locale.US, bytes / 1e6)
            bytes >= 1_000 -> "${bytes / 1000} KB"
            else -> "$bytes B"
        }
        // the box's why is "damaged: <decoder's words>; ffmpeg cannot read it either (…)": the first part says enough
        val short = why.substringBefore(";").removePrefix("damaged: ").take(60)
        return f.format(java.util.Date(takenAt * 1000)) + " UTC · " + size + (if (short.isNotBlank()) " · $short" else "")
    }
}
