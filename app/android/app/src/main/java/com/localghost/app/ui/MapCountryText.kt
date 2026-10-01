package com.localghost.app.ui

/** The words of SETTINGS › MAPS › COUNTRIES. Pure, so the JVM tests read them. */
object MapCountryText {
    fun size(bytes: Long): String = when {
        bytes <= 0 -> "nothing"
        bytes < 1_000_000 -> "${bytes / 1000} KB"
        bytes < 1_000_000_000 -> "${bytes / 1_000_000} MB"
        else -> "%.1f GB".format(java.util.Locale.US, bytes / 1e9)
    }

    fun thousands(n: Int): String = "%,d".format(java.util.Locale.US, n)

    /** A country in the picker: "Greece · 1,234 streets · 40 main · 312 MB", or why it cannot be picked. */
    fun row(name: String, streets: Int, major: Int, coast: Int, bytes: Long): String {
        if (streets + major + coast == 0) return "$name · nothing on the box for it"
        val parts = ArrayList<String>()
        if (streets > 0) parts.add("${thousands(streets)} streets")
        if (major > 0) parts.add("${thousands(major)} main")
        if (coast > 0) parts.add("${thousands(coast)} coast")
        return "$name · " + parts.joinToString(" · ") + " · " + size(bytes)
    }

    /** A picked country's line: "Greece · 812 of 1,274 tiles on this phone (312 MB)". */
    fun progress(name: String, have: Int, total: Int, bytes: Long): String = when {
        total == 0 -> "$name · the box has no tiles for it yet"
        have >= total -> "$name · all ${thousands(total)} tiles on this phone (${size(bytes)})"
        else -> "$name · ${thousands(have)} of ${thousands(total)} tiles on this phone (${size(bytes)} in all)"
    }

    /** The fold's closed line and the picker's header: what is picked, in a few words. */
    fun picked(names: List<String>): String = when (names.size) {
        0 -> "no whole country picked"
        1 -> names[0]
        2 -> names[0] + " and " + names[1]
        else -> names.take(2).joinToString(", ") + " and ${names.size - 2} more"
    }
}
