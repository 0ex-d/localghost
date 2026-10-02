package com.localghost.app.ui

/**
 * WHERE A NOTIFICATION GOES when it is tapped, in the list or in the shade. The box names it
 * (link: "map:2026-09-28", "memories:4182", "memories:near", "news", "status"); a notification
 * from before links goes by who said it and what kind it is. Pure, so the JVM tests read it.
 */
object NotifLink {
    /** [dest]: "map", "memories", "news", "status", "notifications" or "" (nowhere); [arg]: a
     *  day for the map, a memory's id or "near" for memories. */
    data class Target(val dest: String, val arg: String = "")

    private val day = Regex("""^\d{4}-\d{2}-\d{2}$""")

    fun resolve(link: String, service: String, kind: String): Target {
        val l = link.trim()
        if (l.isNotEmpty()) {
            val (head, arg) = l.substringBefore(':') to l.substringAfter(':', "")
            when (head) {
                "map" -> return Target("map", arg.takeIf { day.matches(it) } ?: "")
                "memories" -> return Target("memories", arg.takeIf { it == "near" || it.toLongOrNull() != null } ?: "")
                "news", "status", "notifications" -> return Target(head)
            }
        }
        return when (service.removePrefix("ghost.") to kind) {
            "framed" to "highlight" -> Target("map")
            "cued" to "nearby" -> Target("memories", "near")
            "cued" to "reflection", "secd" to "checkin" -> Target("memories")
            "synthd" to "news" -> Target("news")
            else -> when (service.removePrefix("ghost.")) {
                "watchd", "shadowd" -> Target("status")
                else -> Target("")
            }
        }
    }

    /** The launch extra for a tap in the shade: the link, or the fallback as a link. */
    fun nav(link: String, service: String, kind: String): String {
        val t = resolve(link, service, kind)
        return when {
            t.dest.isEmpty() -> "notifications"
            t.arg.isEmpty() -> t.dest
            else -> t.dest + ":" + t.arg
        }
    }
}

/** What a notification's tap opens, in a few words: "open on MAP ›". Pure, for the tests. */
object NotifText {
    fun opens(t: NotifLink.Target): String = when (t.dest) {
        "map" -> "open on MAP ›"
        "memories" -> if (t.arg == "near") "open near you ›" else "open in MEMORIES ›"
        "news" -> "open NEWS ›"
        "status" -> "open Box Status ›"
        else -> ""
    }
}
