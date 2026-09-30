package com.localghost.app.sync

/**
 * A trail point as a line of text, the shape that is sealed into the spool and the recent ring and
 * opened again on the box: "ts lat lon [acc [via]]". The accuracy stays on the phone; the way the
 * point was taken (via: w the quarter-hour fix, p another app's fix, a the app opening) goes to
 * the box with it. Lines from before via (three or four fields) still read. Pure, so the tests
 * read it.
 */
object TrailLine {
    data class Fields(val ts: Long, val lat: Double, val lon: Double, val acc: Float, val via: String)

    fun format(ts: Long, lat: Double, lon: Double, acc: Float, via: String): String =
        "$ts $lat $lon" + when {
            via.isNotEmpty() -> " ${acc.toInt()} $via"
            acc > 0f -> " ${acc.toInt()}"
            else -> ""
        }

    fun parse(line: String): Fields? {
        val parts = line.trim().split(' ')
        if (parts.size < 3 || parts.size > 5) return null
        val ts = parts[0].toLongOrNull() ?: return null
        val lat = parts[1].toDoubleOrNull() ?: return null
        val lon = parts[2].toDoubleOrNull() ?: return null
        val acc = if (parts.size >= 4) parts[3].toFloatOrNull() ?: 0f else 0f
        val via = if (parts.size == 5) parts[4] else ""
        return Fields(ts, lat, lon, acc, via)
    }
}
