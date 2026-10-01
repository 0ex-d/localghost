package com.localghost.app.sync

import android.content.Context
import com.localghost.app.net.BoxClient
import com.localghost.app.settings.AppSettings
import java.io.ByteArrayOutputStream
import java.net.HttpURLConnection
import java.net.URL

/**
 * THE PHONE FETCHES FOR THE BOX. The box opens no connection, so the news feeds and the market
 * tickers it reads are fetched here, the way the web search round is: the box says what to fetch
 * (/v1/fetch/list, its own list), this fetches each address with a plain browser user agent and
 * no cookies, and hands the bytes over as they came (/v1/news/fetched, /v1/rates/fetched); the
 * parsing, the stories, the index and the digests are the box's. Publishers and exchanges see this
 * phone's address; the box gets nothing while the phone is off. Hourly in the background; the
 * tickers every run, the feeds when their interval has passed (two hours by default).
 */
object BoxFetch {
    private const val PREFS = "lg_boxfetch"
    private const val UA = "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Mobile Safari/537.36"
    private const val CAP = 3_000_000 // bytes per body
    const val WORK = "localghost.fetch"

    data class Outcome(val at: Long, val feeds: Int, val feedsOK: Int, val rates: Int, val ratesOK: Int, val note: String)

    /** One address, as the box will see it: the status (0 when the fetch itself failed), the error, the body. */
    fun fetchAs(id: String, url: String): BoxClient.Fetched {
        var conn: HttpURLConnection? = null
        return try {
            conn = (URL(url).openConnection() as HttpURLConnection).apply {
                requestMethod = "GET"; instanceFollowRedirects = true
                connectTimeout = 10_000; readTimeout = 15_000
                setRequestProperty("User-Agent", UA)
                setRequestProperty("Accept", "application/rss+xml, application/atom+xml, application/xml, application/json, text/xml;q=0.9, */*;q=0.5")
                useCaches = false
            }
            val code = conn.responseCode
            val stream = if (code in 200..299) conn.inputStream else conn.errorStream
            val body = stream?.use { ins ->
                val out = ByteArrayOutputStream()
                val buf = ByteArray(16 * 1024)
                while (true) {
                    val n = ins.read(buf)
                    if (n < 0) break
                    if (out.size() + n > CAP) break
                    out.write(buf, 0, n)
                }
                out.toString("UTF-8")
            } ?: ""
            BoxClient.Fetched(id, code, "", body)
        } catch (e: Exception) {
            BoxClient.Fetched(id, 0, (e.javaClass.simpleName + ": " + (e.message ?: "")).take(160), "")
        } finally { conn?.disconnect() }
    }

    /** One run: ask the box, fetch what is due, hand it over. The box hears the network first, so
     *  it knows the phone is fetching and stays off the wire. */
    suspend fun run(ctx: Context, force: Boolean = false): Outcome {
        val now = System.currentTimeMillis() / 1000
        BoxClient.reportNet(ctx)
        val list = BoxClient.fetchList(ctx) ?: return note(ctx, Outcome(now, 0, 0, 0, 0, "the box did not answer (is it unlocked?)"))
        val p = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        // the tickers: each when its interval has passed
        val due = list.rates.filter { force || now - p.getLong("at.${it.id}", 0) >= it.every * 60L - 60 }
        var ratesOK = 0
        if (due.isNotEmpty()) {
            val rows = due.map { s -> fetchAs(s.id, s.url) }
            ratesOK = rows.count { it.status in 200..299 && it.body.isNotEmpty() }
            if (BoxClient.postRatesFetched(ctx, now, rows)) {
                p.edit().also { e -> due.forEach { e.putLong("at.${it.id}", now) } }.apply()
            }
        }
        // the feeds: all together, when their interval has passed
        var feeds = 0; var feedsOK = 0
        if (list.feeds.isNotEmpty() && (force || now - p.getLong("at.feeds", 0) >= list.feedsEvery * 60L - 60)) {
            val rows = list.feeds.map { s -> fetchAs(s.id, s.url) }
            feeds = rows.size
            feedsOK = rows.count { it.status in 200..299 && it.body.isNotEmpty() }
            if (BoxClient.postNewsFetched(ctx, now, rows)) p.edit().putLong("at.feeds", now).apply()
            else return note(ctx, Outcome(now, feeds, feedsOK, due.size, ratesOK, "fetched, but the box did not take the feeds"))
        }
        return note(ctx, Outcome(now, feeds, feedsOK, due.size, ratesOK, ""))
    }

    private fun note(ctx: Context, o: Outcome): Outcome {
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putLong("last.at", o.at).putInt("last.feeds", o.feeds).putInt("last.feedsOk", o.feedsOK)
            .putInt("last.rates", o.rates).putInt("last.ratesOk", o.ratesOK).putString("last.note", o.note).apply()
        return o
    }

    fun last(ctx: Context): Outcome? {
        val p = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val at = p.getLong("last.at", 0)
        if (at == 0L) return null
        return Outcome(at, p.getInt("last.feeds", 0), p.getInt("last.feedsOk", 0), p.getInt("last.rates", 0), p.getInt("last.ratesOk", 0), p.getString("last.note", "") ?: "")
    }

    /** Hourly while the setting is on, on Wi-Fi only: on mobile data the box fetches for itself
     *  (it hears the network from the quarter-hour poll), so the phone's data is never spent on it. */
    fun schedule(ctx: Context) {
        if (!AppSettings.boxFetch(ctx)) { cancel(ctx); return }
        val req = androidx.work.PeriodicWorkRequestBuilder<FetchWorker>(1, java.util.concurrent.TimeUnit.HOURS)
            .setConstraints(androidx.work.Constraints.Builder().setRequiredNetworkType(androidx.work.NetworkType.UNMETERED).setRequiresBatteryNotLow(true).build())
            .build()
        androidx.work.WorkManager.getInstance(ctx).enqueueUniquePeriodicWork(WORK, androidx.work.ExistingPeriodicWorkPolicy.UPDATE, req)
    }

    /** [ fetch now ]: a run on this network, everything regardless of intervals. */
    fun runNow(ctx: Context) {
        val req = androidx.work.OneTimeWorkRequestBuilder<FetchWorker>()
            .setInputData(androidx.work.Data.Builder().putBoolean("force", true).build())
            .setConstraints(androidx.work.Constraints.Builder().setRequiredNetworkType(androidx.work.NetworkType.CONNECTED).build())
            .build()
        androidx.work.WorkManager.getInstance(ctx).enqueueUniqueWork("$WORK.now", androidx.work.ExistingWorkPolicy.REPLACE, req)
    }

    fun cancel(ctx: Context) {
        androidx.work.WorkManager.getInstance(ctx).cancelUniqueWork(WORK)
    }
}

class FetchWorker(ctx: Context, params: androidx.work.WorkerParameters) : androidx.work.CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        val ctx = applicationContext
        if (!AppSettings.boxFetch(ctx) || !com.localghost.app.security.BoxConfig.isConfigured(ctx)) return Result.success()
        BoxFetch.run(ctx, force = inputData.getBoolean("force", false))
        return Result.success()
    }
}
