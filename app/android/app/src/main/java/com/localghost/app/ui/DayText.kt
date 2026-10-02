package com.localghost.app.ui

import java.util.Calendar
import java.util.Locale
import java.util.TimeZone

/**
 * ONE DAY IN WORDS, for the DAY page: its name, how long ago it was, the days and years either
 * side of it, its bounds on this phone's clock, and the lines under its parts. Pure, so the JVM
 * tests read it. A day is "YYYY-MM-DD".
 */
object DayText {
    private val dayRe = Regex("""^\d{4}-\d{2}-\d{2}$""")

    fun valid(day: String): Boolean = dayRe.matches(day)

    private fun cal(day: String, tz: TimeZone): Calendar? {
        if (!valid(day)) return null
        val (y, m, d) = day.split('-').map { it.toInt() }
        return Calendar.getInstance(tz, Locale.UK).apply {
            clear()
            set(y, m - 1, d, 0, 0, 0)
            isLenient = true
        }
    }

    private fun fmt(c: Calendar): String = "%04d-%02d-%02d".format(Locale.US, c.get(Calendar.YEAR), c.get(Calendar.MONTH) + 1, c.get(Calendar.DAY_OF_MONTH))

    /** The day [n] days on (n < 0: earlier). */
    fun shift(day: String, n: Int, tz: TimeZone = TimeZone.getDefault()): String {
        val c = cal(day, tz) ?: return day
        c.add(Calendar.DAY_OF_MONTH, n)
        return fmt(c)
    }

    /** The same date [n] years on (29 February becomes the 28th). */
    fun shiftYears(day: String, n: Int, tz: TimeZone = TimeZone.getDefault()): String {
        val c = cal(day, tz) ?: return day
        c.add(Calendar.YEAR, n)
        return fmt(c)
    }

    /** The day a moment falls on, on this phone's clock. */
    fun of(unixS: Long, tz: TimeZone = TimeZone.getDefault()): String {
        val c = Calendar.getInstance(tz, Locale.UK).apply { timeInMillis = unixS * 1000 }
        return fmt(c)
    }

    /** The day's start and end (unix seconds, end exclusive) on this phone's clock. */
    fun bounds(day: String, tz: TimeZone = TimeZone.getDefault()): Pair<Long, Long> {
        val c = cal(day, tz) ?: return 0L to 0L
        val start = c.timeInMillis / 1000
        c.add(Calendar.DAY_OF_MONTH, 1)
        return start to c.timeInMillis / 1000
    }

    /** "Thursday 2 October 2025". */
    fun heading(day: String, tz: TimeZone = TimeZone.getDefault()): String {
        val c = cal(day, tz) ?: return day
        val f = java.text.SimpleDateFormat("EEEE d MMMM yyyy", Locale.UK)
        f.timeZone = tz
        return f.format(c.time)
    }

    /** "today", "yesterday", "5 days ago", "3 weeks ago", "1 year ago", "2 years ago", "tomorrow". */
    fun ago(day: String, today: String): String {
        if (!valid(day) || !valid(today)) return ""
        val utc = TimeZone.getTimeZone("UTC")
        val d = (bounds(today, utc).first - bounds(day, utc).first) / 86400
        val years = yearsBetween(day, today)
        return when {
            d == 0L -> "today"
            d == 1L -> "yesterday"
            d == -1L -> "tomorrow"
            d < 0 -> "in ${-d} days"
            years >= 1 && day.substring(5) == today.substring(5) -> if (years == 1) "1 year ago" else "$years years ago"
            d < 14 -> "$d days ago"
            d < 60 -> "${d / 7} weeks ago"
            d < 365 -> "${d / 30} months ago"
            years == 1 -> "over a year ago"
            else -> "$years years ago"
        }
    }

    private fun yearsBetween(day: String, today: String): Int {
        var y = today.substring(0, 4).toInt() - day.substring(0, 4).toInt()
        if (today.substring(5) < day.substring(5)) y--
        return y
    }

    /** "24 photos · 3 videos", "1 photo", "". */
    fun media(photos: Int, videos: Int): String = listOf(
        when (photos) { 0 -> ""; 1 -> "1 photo"; else -> "$photos photos" },
        when (videos) { 0 -> ""; 1 -> "1 video"; else -> "$videos videos" },
    ).filter { it.isNotEmpty() }.joinToString(" · ")

    /** "8,412 steps · slept 7 h 20 min · 45 min of exercise", "". */
    fun body(steps: Int, sleepMinutes: Int, exerciseMinutes: Int): String = listOf(
        if (steps > 0) "%,d steps".format(Locale.UK, steps) else "",
        if (sleepMinutes > 0) "slept " + hm(sleepMinutes) else "",
        if (exerciseMinutes > 0) hm(exerciseMinutes) + " of exercise" else "",
    ).filter { it.isNotEmpty() }.joinToString(" · ")

    private fun hm(m: Int): String = when {
        m < 60 -> "$m min"
        m % 60 == 0 -> "${m / 60} h"
        else -> "${m / 60} h ${m % 60} min"
    }

    /** Whether an outing (unix seconds, start and end) touches the day. */
    fun touches(start: Long, end: Long, day: String, tz: TimeZone = TimeZone.getDefault()): Boolean {
        val (a, b) = bounds(day, tz)
        return start > 0 && start < b && (if (end > 0) end else start) >= a
    }

    /** What the page says when the box has nothing of the day. */
    fun nothing(day: String, today: String): String =
        if (day > today) "this day has not happened yet" else "nothing of this day on the box yet , no photos, no trail, no notes"
}
