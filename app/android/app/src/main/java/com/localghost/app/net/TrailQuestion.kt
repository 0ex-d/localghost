package com.localghost.app.net

/**
 * A stretch of a day's trail the box asks about (framed/questions.go): the trail goes somewhere
 * unlikely and comes straight back. "No, delete it" removes those points from the box for good;
 * "yes" keeps them and the box never asks about that stretch again. Pure, so the tests read it.
 *
 * [kind]: "glitch" (already left off the map by the glitch rules), "fast" (drawn, but the legs
 * need more than 200 km/h) or "offroad" (drawn, but no road goes there).
 */
data class TrailQuestion(
    val from: Long, val to: Long, val ts: LongArray, val lat: Double, val lon: Double,
    val awayM: Double, val goneS: Long, val kmh: Double, val kind: String, val place: String,
) {
    /** The question as the day panel shows it; [clock] formats a unix second as the phone's time. */
    fun text(clock: (Long) -> String): String {
        val km = if (awayM >= 10_000) "${(awayM / 1000).toInt()} km" else "%.1f km".format(java.util.Locale.US, awayM / 1000)
        val where = if (place.isNotBlank()) "$place, $km away" else "a place $km away"
        val back = when {
            goneS <= 0 -> " and the day ends there"
            goneS < 3600 -> " and is back ${(goneS + 30) / 60} min later"
            else -> " and is back ${goneS / 3600} h ${(goneS % 3600) / 60} min later"
        }
        val why = when (kind) {
            "fast" -> ". That would take ${kmh.toInt()} km/h."
            "offroad" -> ". No road goes there."
            "glitch" -> ". It is already left off the map."
            else -> "."
        }
        return "At ${clock(from)} the trail goes to $where$back$why Were you there?"
    }

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
                    o.optDouble("awayM", 0.0), o.optLong("goneS", 0L), o.optDouble("kmh", 0.0), o.optString("kind", ""), o.optString("place", ""))
            }
        }
    }
}
