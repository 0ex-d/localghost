package com.localghost.app.phrases

import android.content.Context
import com.localghost.app.net.BoxClient
import org.json.JSONArray
import org.json.JSONObject

/**
 * The home brief the lock-screen card shows at home: kept on the phone (the card is drawn by an
 * alarm and a receiver, with no box at hand), fetched from the box with the notification poll
 * every quarter hour and when the app opens. The box picked the stories and made the prices; the
 * phone only chooses which to show (HomeBriefText).
 */
object HomeBrief {
    private const val PREFS = "lg_home_brief"

    data class Kept(val at: Long, val cards: List<HomeBriefText.Card>, val prices: String, val chip: String)

    fun kept(ctx: Context): Kept? = runCatching {
        val s = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString("brief", null) ?: return null
        val o = JSONObject(s)
        val arr = o.optJSONArray("cards") ?: JSONArray()
        Kept(o.optLong("at"), (0 until arr.length()).map { i ->
            val c = arr.getJSONObject(i)
            HomeBriefText.Card(c.optString("id"), c.optString("h"), c.optString("s"), c.optString("o"))
        }, o.optString("prices"), o.optString("chip"))
    }.getOrNull()

    private fun keep(ctx: Context, k: Kept) {
        val arr = JSONArray()
        k.cards.forEach { c -> arr.put(JSONObject().put("id", c.id).put("h", c.headline).put("s", c.summary).put("o", c.outlets)) }
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putString("brief", JSONObject().put("at", k.at).put("cards", arr).put("prices", k.prices).put("chip", k.chip).toString()).apply()
    }

    /** Ask the box for the stories and the prices, keep what came, redraw the card. A box out of
     *  reach keeps the last brief (the card says how old it is when pulled open). */
    suspend fun fetch(ctx: Context): Boolean {
        val app = ctx.applicationContext
        val now = System.currentTimeMillis() / 1000
        val news = BoxClient.news(app, since = now - 86_400)
        val rates = BoxClient.rates(app)
        if (news == null && rates == null) return false
        val old = kept(app)
        val cards = news?.let { n ->
            HomeBriefText.cards(n.stories.map { s ->
                HomeBriefText.Story(s.id, s.title, s.summary, s.items.map { it.outlet }, s.lastSeen, s.sources)
            }, now)
        } ?: old?.cards ?: emptyList()
        val prices = rates?.index?.map { HomeBriefText.Price(it.symbol, it.price, it.change24, it.at) }
        keep(app, Kept(now, cards, prices?.let { HomeBriefText.prices(it) } ?: old?.prices ?: "", prices?.let { HomeBriefText.chip(it) } ?: old?.chip ?: ""))
        if (PhraseState.lockScreenOn(app)) PhraseSurface.refresh(app)
        return true
    }

    /** Whether the phone is at home: the network country is home's, or unknown (a phone with no
     *  SIM on Wi-Fi is most likely at home; abroad the network names the country). */
    fun atHome(ctx: Context): Boolean {
        val home = PhraseOffer.homeCountry(ctx)
        val where = CountryDetect.detect(ctx)
        if (where.source == "unknown" || where.country.isEmpty()) return true
        return home.isNotEmpty() && where.country.equals(home, ignoreCase = true)
    }
}
