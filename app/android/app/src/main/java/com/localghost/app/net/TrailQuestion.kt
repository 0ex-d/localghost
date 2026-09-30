package com.localghost.app.net

/**
 * A stretch of a day's trail the box asks about (framed/questions.go): the trail goes somewhere
 * unlikely and comes straight back. "No, delete it" removes those points from the box for good;
 * "yes" keeps them and the box never asks about that stretch again. Pure, so the tests read it.
 *
 * [kind]: "glitch" (already left off the map by the glitch rules), "sea" (drawn, but a hop runs
 * across [seaM] metres of open water faster than a boat), "fast" (drawn, but a leg needs 90 km/h
 * or more between two fixes) or "offroad" (drawn, but no road goes there). Visits to the same place
 * in one afternoon come as one question; [ts] holds every fix a "no" deletes.
 */
data class TrailQuestion(
    val from: Long, val to: Long, val ts: LongArray, val lat: Double, val lon: Double,
    val awayM: Double, val goneS: Long, val kmh: Double, val kind: String, val place: String,
    val seaM: Double = 0.0,
) {
    /** The question as the day panel shows it; [clock] formats a unix second as the phone's time. */
    fun text(clock: (Long) -> String): String {
        val away = km(awayM)
        val where = if (place.isNotBlank()) "$place, $away away" else "a place $away away"
        val back = when {
            goneS <= 0 -> " and the day ends there"
            goneS < 3600 -> " and is back ${(goneS + 30) / 60} min later"
            else -> " and is back ${goneS / 3600} h ${(goneS % 3600) / 60} min later"
        }
        val fixes = if (ts.size > 1 && to > from) " (${ts.size} fixes there until ${clock(to)})" else ""
        val why = when (kind) {
            "sea" -> ". That is ${km(seaM)} of open sea at ${kmh.toInt()} km/h."
            "fast" -> ". That would take ${kmh.toInt()} km/h."
            "offroad" -> ". No road goes there."
            "glitch" -> ". It is already left off the map."
            else -> "."
        }
        return "At ${clock(from)} the trail goes to $where$fixes$back$why Were you there?"
    }

    private fun km(m: Double): String =
        if (m >= 10_000) "${(m / 1000).toInt()} km" else "%.1f km".format(java.util.Locale.US, m / 1000)

    override fun equals(other: Any?): Boolean = other is TrailQuestion && other.from == from && other.to == to
    override fun hashCode(): Int = (from * 31 + to).hashCode()

    companion object {
        fun listFrom(a: org.json.JSONArray?): List<TrailQuestion> {
            if (a == null) return emptyList()
            return (0 until a.length()).mapNotNull { i ->
                val o = a.optJSONObject(i) ?: return@mapNotNull null
                val t = o.optJSONArray("ts") ?: return@mapNotNull null
                val from = o.optLong("from", 0L)
                val to = o.optLong("to", 0L)
                if (from <= 0 || to < from || t.length() == 0) return@mapNotNull null
                TrailQuestion(from, to, LongArray(t.length()) { t.optDouble(it, 0.0).toLong() }, o.optDouble("lat", 0.0), o.optDouble("lon", 0.0),
                    o.optDouble("awayM", 0.0), o.optLong("goneS", 0L), o.optDouble("kmh", 0.0), o.optString("kind", ""), o.optString("place", ""),
                    o.optDouble("seaM", 0.0))
            }
        }
    }
}
