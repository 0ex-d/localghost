package com.localghost.app.net

/** The learned stage times, kept on this phone between unlocks (stage name -> ms). */
object UnlockClockStore {
    private const val PREFS = "unlock_clock"

    fun load(ctx: android.content.Context): Map<String, Long> = try {
        ctx.getSharedPreferences(PREFS, android.content.Context.MODE_PRIVATE).all
            .mapNotNull { (k, v) -> (v as? Long)?.let { k to it } }.toMap()
    } catch (_: Exception) { emptyMap() }

    fun save(ctx: android.content.Context, m: Map<String, Long>) {
        val e = ctx.getSharedPreferences(PREFS, android.content.Context.MODE_PRIVATE).edit()
        m.forEach { (k, v) -> e.putLong(k, v) }
        e.apply()
    }
}
