package com.localghost.app.ui

import java.util.Locale

/**
 * A COIN'S PAGE IN WORDS: the chart's ranges and where each comes from on the box, the figures
 * under the price, and how the box made the price, market by market. Pure, so the JVM tests read it.
 */
object CoinText {
    /** A chart range: its label, and which series it reads (the minute or the hour series for so
     *  many hours, or the daily closes for so many days). */
    enum class Range(val label: String, val res: String, val hours: Int, val days: Int) {
        DAY("1D", "1m", 24, 0),
        WEEK("1W", "1h", 168, 0),
        MONTH("1M", "1h", 720, 0),
        YEAR("1Y", "", 0, 365),
        ALL("ALL", "", 0, 5000),
    }

    /** At most [max] points, evenly picked, the last always kept (a chart has so many pixels). */
    fun <T> thin(points: List<T>, max: Int = 240): List<T> {
        if (points.size <= max || max < 2) return points
        val step = (points.size - 1).toDouble() / (max - 1)
        return (0 until max).map { i -> points[Math.round(i * step).toInt().coerceAtMost(points.size - 1)] }
    }

    /** The change from the first close to the last, in per cent; null with fewer than two. */
    fun changeOver(closes: List<Double>): Double? {
        val a = closes.firstOrNull { it > 0 } ?: return null
        val b = closes.lastOrNull { it > 0 } ?: return null
        if (closes.count { it > 0 } < 2) return null
        return 100 * (b / a - 1)
    }

    /** "$1.73T", "$38.3B", "$412.0M": a dollar amount for the figures. */
    fun dollars(v: Double): String = if (v <= 0) "—" else "$" + HomeText.cap(v)

    /** "19.9M BTC", "580.2M SOL", "45,000 X": a supply in coins. */
    fun supply(v: Double, symbol: String): String = when {
        v <= 0 -> "—"
        v >= 1e9 -> "%.2fB %s".format(Locale.US, v / 1e9, symbol)
        v >= 1e6 -> "%.1fM %s".format(Locale.US, v / 1e6, symbol)
        else -> "%,d %s".format(Locale.US, Math.round(v), symbol)
    }

    /** "from 9 markets on 6 exchanges · USD, USDT, EUR, BTC": how the box made the price. */
    fun madeFrom(markets: Int, exchanges: Int, paths: String): String {
        if (exchanges <= 0) return ""
        val m = if (markets > exchanges) "$markets markets on " else ""
        val e = if (exchanges == 1) "1 exchange" else "$exchanges exchanges"
        val p = paths.split(',').map { it.trim() }.filter { it.isNotEmpty() }
        return "from $m$e" + (if (p.isNotEmpty()) " · " + p.joinToString(", ") else "")
    }

    /** "coinbase · BTC/USD", "kraken · SOL/BTC": one market. */
    fun market(exchange: String, symbol: String, quote: String): String = "$exchange · $symbol/$quote"

    /** "42%", "<1%": a market's share of the price. */
    fun share(w: Double): String = when {
        w <= 0 -> ""
        w < 0.01 -> "<1%"
        else -> "${Math.round(w * 100)}%"
    }

    /** "12 s", "4 min": how old a market's price is. */
    fun age(s: Long): String = if (s < 90) "$s s" else "${(s + 30) / 60} min"

    /** The price at a point of the chart, with when: "84,512 · Fri 3 Oct 14:05" (minutes and hours) or "84,512 · 3 Oct 2025". */
    fun at(price: Double, t: Long, daily: Boolean): String {
        val f = java.text.SimpleDateFormat(if (daily) "d MMM yyyy" else "EEE d MMM HH:mm", Locale.UK)
        return HomeText.money(price) + " · " + f.format(java.util.Date(t * 1000))
    }

    /** Under the coin's text: "written by your box from Wikipedia, Coinbase and solana.com", or
     *  "Coinbase's description" while the box has not written one; "" with neither. */
    fun writtenBy(written: String, from: String, coinbase: String): String {
        if (written.isNotBlank()) {
            val f = from.split(',').map { it.trim() }.filter { it.isNotEmpty() }
            return "written by your box" + when (f.size) {
                0 -> ""
                1 -> " from " + f[0]
                else -> " from " + f.dropLast(1).joinToString(", ") + " and " + f.last()
            }
        }
        return if (coinbase.isNotBlank()) "Coinbase's description" else ""
    }

    /** "#5" for the rank, "" without one. */
    fun rank(r: Int): String = if (r > 0) "#$r" else ""

    /** A colour from "#9945FF", null when it is not one. */
    fun color(hex: String): Long? {
        if (hex.length != 7 || hex[0] != '#') return null
        return hex.substring(1).toLongOrNull(16)?.let { 0xFF000000L or it }
    }
}
