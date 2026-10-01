package com.localghost.app.ui

/** The words of the NEWS screen. Pure, so the JVM tests read them. */
object NewsText {
    fun ago(sec: Long, now: Long): String {
        if (sec <= 0) return "never"
        val d = now - sec
        return when {
            d < 60 -> "just now"
            d < 3600 -> "${d / 60} min ago"
            d < 48 * 3600 -> "${d / 3600} h ago"
            else -> "${d / 86400} days ago"
        }
    }

    /** "BBC · The Guardian · 2 h ago" , the outlets telling a story, newest entry's age. */
    fun outlets(names: List<String>, newest: Long, now: Long): String {
        val distinct = names.filter { it.isNotEmpty() }.distinct()
        val shown = if (distinct.size <= 3) distinct else distinct.take(3) + listOf("${distinct.size - 3} more")
        return (shown + listOf(ago(newest, now))).joinToString(" · ")
    }

    /** The header's status: when the phone last fetched, when the last digest went. */
    fun status(lastFetch: Long, lastDigest: Long, now: Long, fetching: Boolean): String {
        if (fetching) return "fetching the feeds on this phone…"
        if (lastFetch == 0L) return "nothing fetched yet , the phone fetches the feeds every two hours while it is on"
        val d = if (lastDigest == 0L) "no digest yet (07:00 and 19:00)" else "last digest " + ago(lastDigest, now)
        return "feeds fetched " + ago(lastFetch, now) + " · " + d
    }

    /** One symbol's price for the markets line: symbol, USD price, venues in, when. */
    data class Price(val symbol: String, val usd: Double, val n: Int, val at: Long)

    /** The market index for the line: "crypto 1,234.5 (+1.23% today, 50 coins)". */
    fun market(value: Double, dayChange: Double, constituents: Int): String =
        if (value <= 0) "" else "crypto " + "%,.1f".format(java.util.Locale.US, value) + " (" + "%+.2f".format(java.util.Locale.US, dayChange) + "% today, $constituents coins)"

    /** One line of market numbers: the index for crypto as a whole, the symbols the box follows (BTC first) and two rates, or what is missing. */
    fun markets(prices: List<Price>, fx: Map<String, Double>, fxDay: String, now: Long, market: String = ""): String {
        val parts = ArrayList<String>()
        if (market.isNotEmpty()) parts.add(market)
        // BTC and ETH only: the other forty-eight are on CRYPTO, a tap from home
        val ordered = prices.filter { it.usd > 0 && (it.symbol == "BTC" || it.symbol == "ETH") }.sortedWith(compareBy({ it.symbol != "BTC" }, { it.symbol }))
        ordered.take(3).forEach { p -> parts.add(p.symbol + " " + money(p.usd) + " USD (" + p.n + " venues, " + ago(p.at, now) + ")") }
        prices.firstOrNull { it.symbol == "USDT" && it.usd > 0 }?.let { parts.add("USDT " + "%.4f".format(java.util.Locale.US, it.usd)) }
        fx["GBP"]?.let { parts.add("EUR/GBP " + "%.4f".format(java.util.Locale.US, it)) }
        fx["RON"]?.let { parts.add("EUR/RON " + "%.4f".format(java.util.Locale.US, it)) }
        if (fx.isNotEmpty() && fxDay.isNotEmpty()) parts.add("ECB $fxDay")
        return if (parts.isEmpty()) "no market numbers on the box yet" else parts.joinToString(" · ")
    }

    fun money(v: Double): String = when {
        v <= 0 -> "0"
        v < 1 -> "%.4f".format(java.util.Locale.US, v)
        v < 1000 -> "%.2f".format(java.util.Locale.US, v)
        else -> "%,d".format(java.util.Locale.US, Math.round(v))
    }
}
