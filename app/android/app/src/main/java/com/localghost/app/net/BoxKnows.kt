package com.localghost.app.net

/**
 * WHAT THE BOX KEEPS, judged on the phone when the box's plan did not come in time: the price of
 * a coin, the top coins, crypto as a whole, a rate between currencies. The box keeps all of these
 * (a minute old, from seven exchanges; Coinbase's rank list; the ECB's table) and puts them in its
 * answer, so the phone does not search the web for them. The box's own judgement (/v1/chat/plan,
 * boxHas) knows every coin it follows; this one knows the common ones. Pure, for the JVM tests.
 */
object BoxKnows {
    private val coin = Regex("\\b(btc|bitcoin|eth|ether|ethereum|sol|solana|xrp|ripple|doge|dogecoin|ada|cardano|ltc|litecoin|crypto|cryptos|cryptocurrenc(y|ies))\\b", RegexOption.IGNORE_CASE)
    private val priceWords = Regex("\\b(price|prices|worth|cost|trading|value|how much|doing|up|down|today|now|chart|market cap|usd|dollars?|euros?|pounds?)\\b|[$€£]", RegexOption.IGNORE_CASE)
    private val top = Regex("\\b(top|biggest|largest|leading)\\s+(\\d{1,3}\\s+)?(coins?|cryptos?|crypto ?currenc(y|ies)|tokens?|altcoins?)\\b", RegexOption.IGNORE_CASE)
    private val fiat = Regex("\\b(eur|euros?|usd|dollars?|gbp|pounds?|sterling|ron|lei|chf|francs?|jpy|yen)\\b", RegexOption.IGNORE_CASE)
    private val rate = Regex("\\b(exchange rate|convert|in (euros?|dollars|pounds|lei|yen|francs))\\b|\\bto (eur|usd|gbp|ron|chf|jpy)\\b", RegexOption.IGNORE_CASE)

    fun covers(q: String): Boolean {
        if (top.containsMatchIn(q)) return true
        if (coin.containsMatchIn(q) && (priceWords.containsMatchIn(q) || q.trim().split(Regex("\\s+")).size <= 3)) return true
        return rate.containsMatchIn(q) && fiat.findAll(q).count() >= 1
    }
}
