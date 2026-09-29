package com.localghost.app.local

import java.util.Locale

/**
 * Speed and time left for a long transfer (a phone model from the box: gigabytes over Wi-Fi).
 *
 * The rate is measured over the last [windowMs] of samples, not since the start: a resumed
 * download, or Wi-Fi that got better halfway, shows what is happening now. The shown rate is then
 * smoothed so the number does not flicker every half second, and the time left is rounded to a
 * step that suits its size (5 s under a minute, whole minutes under an hour). Pure: the worker
 * feeds it; the tests drive it with their own clock.
 */
class TransferRate(private val windowMs: Long = 8_000, private val smoothing: Double = 0.35) {
    private val times = ArrayDeque<Long>()
    private val bytes = ArrayDeque<Long>()
    private var shown = -1.0

    /** Adds a sample: [done] bytes transferred so far (the file's length, resume included) at [nowMs]. */
    fun add(nowMs: Long, done: Long) {
        times.addLast(nowMs); bytes.addLast(done)
        while (times.size > 2 && nowMs - times.first() > windowMs) { times.removeFirst(); bytes.removeFirst() }
        val span = times.last() - times.first()
        if (span < 1_000) return // under a second of samples says nothing yet
        val raw = (bytes.last() - bytes.first()) * 1000.0 / span
        shown = if (shown < 0) raw else shown + smoothing * (raw - shown)
    }

    /** Bytes per second now, or -1 until a second of samples has arrived. */
    fun bytesPerSecond(): Long = if (shown < 0) -1 else shown.toLong()

    /** Seconds until [total] at the current rate, or -1 when unknown (no rate yet, or stalled). */
    fun secondsLeft(done: Long, total: Long): Long {
        val r = bytesPerSecond()
        if (r <= 0 || total <= 0) return -1
        return ((total - done).coerceAtLeast(0) + r - 1) / r
    }

    companion object {
        /** "850 KB/s", "12.4 MB/s"; decimal units, as the sizes on the screen are. */
        fun rate(bytesPerSecond: Long): String = when {
            bytesPerSecond < 0 -> "measuring…"
            bytesPerSecond < 1_000 -> "$bytesPerSecond B/s"
            bytesPerSecond < 1_000_000 -> "${bytesPerSecond / 1_000} KB/s"
            else -> String.format(Locale.US, "%.1f MB/s", bytesPerSecond / 1_000_000.0)
        }

        /** "about 40 s left", "about 3 min left", "about 1 h 12 min left"; "" when unknown. */
        fun left(seconds: Long): String = when {
            seconds < 0 -> ""
            seconds < 5 -> "a few seconds left"
            seconds < 60 -> "about ${((seconds + 4) / 5) * 5} s left"
            seconds < 3_600 -> "about ${(seconds + 59) / 60} min left"
            else -> {
                val m = (seconds + 59) / 60
                if (m % 60 == 0L) "about ${m / 60} h left" else "about ${m / 60} h ${m % 60} min left"
            }
        }

        /** "1.2 GB", "640 MB": the size as a person reads it. */
        fun size(bytes: Long): String = when {
            bytes >= 1_000_000_000 -> String.format(Locale.US, "%.2f GB", bytes / 1_000_000_000.0)
            bytes >= 1_000_000 -> "${bytes / 1_000_000} MB"
            else -> "${bytes / 1_000} KB"
        }
    }
}
