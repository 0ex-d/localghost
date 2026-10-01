package com.localghost.app.ui

/**
 * The trail's two status lines in SETTINGS, and the "near you" source line: the phone's newest fix
 * it can read, and how the last hand-over to the box went. Pure, so the tests read it.
 */
object TrailStatus {
    /** "12 min", "3 h", "2 days". */
    fun ago(sec: Long): String = when {
        sec < 90 -> "just now"
        sec < 3600 -> "${sec / 60} min ago"
        sec < 2 * 86400 -> "${sec / 3600} h ago"
        else -> "${sec / 86400} days ago"
    }

    fun fixLine(fixTs: Long?, now: Long, lastState: String = ""): String = when {
        fixTs != null && fixTs > 0 -> "last fix: " + ago(now - fixTs)
        lastState == "unreadable" -> "last fix: kept, but this phone's own key will not open it (a restore from another phone, or the Keystore refused) , the next fix re-seals it"
        else -> "last fix: none this phone can read yet , the next one comes within the quarter hour"
    }

    /** How the last hand-over went; [waiting] is what the spool still holds. */
    fun sendLine(at: Long?, what: String?, ok: Boolean?, lastOkAt: Long?, waiting: Int, now: Long): String {
        if (at == null || at <= 0) return if (waiting > 0) "to the box: not tried yet , $waiting waiting" else "to the box: not tried yet"
        val s = StringBuilder("to the box: ").append(what ?: "").append(" · ").append(ago(now - at))
        if (ok != true && lastOkAt != null && lastOkAt > 0) s.append(" · last sent ").append(ago(now - lastOkAt))
        if (ok != true && (lastOkAt == null || lastOkAt <= 0)) s.append(" · never sent from this phone")
        return s.toString()
    }

    /** Where "near you" is measured from. */
    fun nearFrom(fromBox: Boolean, fixTs: Long, now: Long): String =
        (if (fromBox) "around the box's newest trail point, " else "around this phone's last fix, ") + ago(now - fixTs)

    /** What each way of taking a point is called on the phone and in the box's report. */
    fun viaName(via: String): String = when (via) {
        "w" -> "quarter-hour"
        "p" -> "other apps' fixes"
        "a" -> "app opened"
        else -> "unmarked"
    }

    /** Today on this phone: kept, by how, sent to the box, still waiting. */
    fun todayLine(kept: Int, byVia: Map<String, Int>, sentToday: Int, waiting: Int): String {
        val how = listOf("w", "p", "a").mapNotNull { v -> byVia[v]?.takeIf { it > 0 }?.let { "$it ${viaName(v)}" } }
        val s = StringBuilder("today: $kept kept")
        if (how.isNotEmpty()) s.append(" (").append(how.joinToString(" · ")).append(")")
        s.append(" · $sentToday sent to the box")
        s.append(if (waiting > 0) " · $waiting waiting" else " · none waiting")
        return s.toString()
    }

    /** What the phone holds: the last two days, synced or not, and everything it ever handed over. */
    fun holdsLine(ringPoints: Int?, sentTotal: Long): String =
        (if (ringPoints == null) "on this phone: the last two days (unlock to count them)" else "on this phone: $ringPoints points from the last two days") +
            " · $sentTotal sent to the box since the trail began"
}
