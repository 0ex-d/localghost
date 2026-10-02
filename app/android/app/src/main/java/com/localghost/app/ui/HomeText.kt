package com.localghost.app.ui

import java.util.Locale

/**
 * THE HOME SCREEN'S WORDS. Home is where the app opens: BTC and ETH always (the other coins a tap
 * away, on CRYPTO), the day's news as one point per story (the box's brief, written from the
 * summaries of the most-told stories, each point opening its story), and a box to ask from. The
 * box made every number and sentence; the phone only picks and writes them. Pure, so the JVM
 * tests read it.
 */
object HomeText {
    /**
     * A coin as home and CRYPTO show it: the box's own price where it has one, the list's
     * otherwise. [supply] is Coinbase's circulating supply; the cap is the shown price times it.
     */
    data class Coin(val rank: Int, val symbol: String, val name: String, val usd: Double, val change24: Double?, val cap: Double, val supply: Double = 0.0)

    /** One point of the brief and the story it tells (null when the points and stories do not pair up). */
    data class Point(val text: String, val story: Long?)

    /**
     * The brief as points: its "- " lines, the nth paired with the nth of [stories] when the counts
     * agree. A brief from before the points is one point of its own.
     */
    fun points(brief: String, stories: List<Long>): List<Point> {
        val t = NewsText.told(brief)
        val lines = if (t.points.isEmpty()) listOfNotNull(t.lead.takeIf { it.isNotEmpty() }) else listOfNotNull(t.lead.takeIf { it.isNotEmpty() }) + t.points
        val paired = t.lead.isEmpty() && t.points.size == stories.size
        return lines.mapIndexed { i, s -> Point(s, if (paired) stories[i] else null) }
    }

    /** The box's prices with the fast lane's over them (BTC, ETH, SOL, seconds old). */
    fun withFast(live: Map<String, Pair<Double, Double?>>, fast: Map<String, Pair<Double, Double?>>): Map<String, Pair<Double, Double?>> =
        live + fast.filterValues { it.first > 0 }

    /** "84,498", "2,692", "0.2512", "169.71". */
    fun money(v: Double): String = when {
        v <= 0 -> "0"
        v < 1 -> "%.4f".format(Locale.US, v)
        v < 1000 -> "%.2f".format(Locale.US, v)
        else -> "%,d".format(Locale.US, Math.round(v))
    }

    /** "+0.9%", "-1.2%"; "" when the box has no change to say. */
    fun change(c: Double?): String = c?.let { "%+.1f%%".format(Locale.US, it) } ?: ""

    /** A market cap: "1.69T", "412.0B", "88.1M". */
    fun cap(v: Double): String = when {
        v >= 1e12 -> "%.2fT".format(Locale.US, v / 1e12)
        v >= 1e9 -> "%.1fB".format(Locale.US, v / 1e9)
        v >= 1e6 -> "%.1fM".format(Locale.US, v / 1e6)
        v > 0 -> money(v)
        else -> ""
    }

    /** The two coins home always shows, BTC first; the rest wait for CRYPTO. */
    fun pinned(coins: List<Coin>): List<Coin> = listOf("BTC", "ETH").mapNotNull { s -> coins.firstOrNull { it.symbol == s && it.usd > 0 } }

    /**
     * The list CRYPTO shows: the rank list's order, each coin at the box's own price (a minute old)
     * where the box follows it, the list's hourly price otherwise; at most [n].
     */
    fun merge(ranks: List<Coin>, live: Map<String, Pair<Double, Double?>>, n: Int = 50): List<Coin> =
        ranks.sortedBy { it.rank }.take(n).map { c ->
            val l = live[c.symbol]
            val m = if (l != null && l.first > 0) c.copy(usd = l.first, change24 = l.second ?: c.change24) else c
            if (m.supply > 0 && m.usd > 0) m.copy(cap = m.usd * m.supply) else m
        }

    /** The home row for a pinned coin with no rank list yet: the box's own price alone. */
    fun liveOnly(symbol: String, usd: Double, change24: Double?): Coin = Coin(0, symbol, symbol, usd, change24, 0.0)

    /** "written 23 min ago" for the brief; "" before the first. */
    fun written(at: Long, now: Long): String {
        if (at <= 0) return ""
        val d = (now - at).coerceAtLeast(0)
        return "written " + when {
            d < 90 -> "just now"
            d < 90 * 60 -> "${(d + 30) / 60} min ago"
            d < 48 * 3600 -> "${(d + 1800) / 3600} h ago"
            else -> "${d / 86400} days ago"
        }
    }

    /** What home says in the brief's place before the box has written one. */
    fun noBrief(stories: Int): String = when (stories) {
        0 -> "no news on the box yet: pull down to fetch the feeds"
        else -> "the box writes the day's brief once a few stories have their summaries"
    }
}
