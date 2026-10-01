package com.localghost.app.local

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.localghost.app.net.BoxClient
import com.localghost.app.settings.AppSettings
import java.io.File
import java.util.concurrent.TimeUnit

/**
 * MAPS ON THE PHONE, AHEAD OF TIME. With "download maps" ticked in SETTINGS, once a day on Wi-Fi
 * (and not on a low battery) the phone fills its map caches from the box: the world outlines, the
 * coast and road indexes, then tiles in [MapPlan]'s order (streets around where the phone has been,
 * then coast and major roads outwards) until the size picked in SETTINGS is reached. The map then
 * opens from the phone's own disk. Tiles already on the phone are skipped, so a run picks up
 * where the last one stopped; a run ends itself after [RUN_MS] and asks to be run again soon when
 * there is more to fetch. Nothing is fetched from anywhere but the box.
 */
class MapPrefetchWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {

    override suspend fun doWork(): Result {
        val ctx = applicationContext
        if (!AppSettings.mapDownload(ctx)) return Result.success()
        val budget = AppSettings.mapBudgetMB(ctx).toLong() * 1_000_000
        val t0 = System.currentTimeMillis()
        MapPrefetch.note(ctx, "running", "")

        // the outlines every zoom starts from, and the two indexes (ETag-checked: cheap when current)
        for (res in listOf("", "50m", "110m")) runCatching { BoxClient.worldGeoJsonFile(ctx, res) }
        val land = runCatching { BoxClient.landTileIndex(ctx) }.getOrNull()
        val roads = com.localghost.app.ui.RoadTileGeom.index(runCatching { BoxClient.roadTileIndex(ctx) }.getOrNull())
        if (land == null && roads == null) {
            MapPrefetch.note(ctx, "idle", "the box has no map tiles cut, or could not be reached")
            return Result.success()
        }

        // WHOLE COUNTRIES FIRST: the ones picked in SETTINGS, every tile the box holds for them,
        // outside the size budget (the size was shown when they were picked). The box says which
        // cells have a tile; a country it cannot describe right now keeps its last progress line.
        val picked = AppSettings.mapCountries(ctx)
        val countryTiles = ArrayList<MapPlan.Tile>()
        var countryBytes = 0L
        var described = 0
        for (code in picked) {
            val c = runCatching { BoxClient.countryCells(ctx, code) }.getOrNull() ?: continue
            described++
            val tiles = MapPlan.countryTiles(c)
            countryBytes += c.bytes
            MapPrefetch.noteCountry(ctx, c.code, c.name, tiles.count { MapPrefetch.onPhone(ctx, it) }, tiles.size, c.bytes)
            countryTiles.addAll(tiles)
        }
        MapPrefetch.noteCountryBytes(ctx, countryBytes)

        // WHERE TO FETCH AROUND: the phone's own points when it can read them (the recent ring
        // opens only while the app is unlocked; the sealed last point always), else the box's
        // trail (the newest days' tracks), else where the map was last looking. The daily run
        // happens with the app locked, so it used to see no fix and say "no location on this
        // phone yet" to a person with a trail on.
        var fixes = com.localghost.app.sync.LocationLog.recent(ctx, 0).map { it.lat to it.lon } +
            listOfNotNull(com.localghost.app.sync.LocationLog.newest(ctx)?.let { it.lat to it.lon })
        var from = "your trail on this phone"
        if (fixes.isEmpty()) {
            val tracks = runCatching { BoxClient.geoDayTracks(ctx, 5) }.getOrNull() ?: emptyList()
            fixes = tracks.flatMap { t -> (0 until t.n).map { t.lat[it] to t.lon[it] } }
            from = "your trail on the box"
        }
        val centers = MapPlan.centers(fixes)
        if (centers.isEmpty() && countryTiles.isEmpty()) {
            MapPrefetch.note(ctx, "idle", if (picked.isEmpty())
                "nothing to go on yet: no fix on this phone and no trail on the box , the outlines are here, tiles follow once the box knows where you have been, or pick a country"
                else "the box could not describe the countries you picked (is it unlocked?)")
            return Result.success()
        }
        val around = if (centers.isEmpty()) emptyList() else MapPlan.plan(centers, land, roads)
        var used = MapPrefetch.bytesOnPhone(ctx)
        var fetched = 0
        var failedInARow = 0
        val haveByCountry = HashMap<String, Int>()
        val totalByCountry = HashMap<String, Int>()
        for (t in countryTiles) totalByCountry[t.country] = (totalByCountry[t.country] ?: 0) + 1
        // one pass over both lists: a country's tiles ignore the budget, the rest stop at it
        val seen = HashSet<String>()
        val all = countryTiles + around
        for (t in all) {
            if (!seen.add(MapPlan.key(t))) continue
            if (isStopped) return Result.retry()
            if (t.country.isEmpty() && used >= budget) break
            if (MapPrefetch.onPhone(ctx, t)) {
                if (t.country.isNotEmpty()) haveByCountry[t.country] = (haveByCountry[t.country] ?: 0) + 1
                continue
            }
            if (System.currentTimeMillis() - t0 > RUN_MS) {
                MapPrefetch.note(ctx, "partial", "$fetched tiles this run; more on the next")
                return Result.retry() // more to fetch: soon, not tomorrow
            }
            val b = runCatching {
                if (t.kind == "land") BoxClient.landTile(ctx, t.x, t.y) else BoxClient.roadTile(ctx, t.level, t.x, t.y)
            }.getOrNull()
            if (b == null) {
                if (++failedInARow >= 5) {
                    MapPrefetch.note(ctx, "idle", "the box stopped answering (locked?) , trying again later")
                    return Result.success()
                }
                continue
            }
            failedInARow = 0
            used += b.size
            fetched++
            if (t.country.isNotEmpty()) {
                val have = (haveByCountry[t.country] ?: 0) + 1
                haveByCountry[t.country] = have
                // the country's own line follows the run too
                if (have % 25 == 0) MapPrefetch.noteCountryHave(ctx, t.country, have, totalByCountry[t.country] ?: 0)
            }
            // SETTINGS shows this live while it runs
            if (fetched % 10 == 0) MapPrefetch.note(ctx, "running", "$fetched tiles so far this run")
        }
        for ((code, total) in totalByCountry) MapPrefetch.noteCountryHave(ctx, code, haveByCountry[code] ?: 0, total)
        val countriesDone = picked.isNotEmpty() && described == picked.size &&
            totalByCountry.all { (code, total) -> (haveByCountry[code] ?: 0) >= total }
        MapPrefetch.note(ctx, "done", when {
            centers.isEmpty() -> "$fetched tiles this run; " + (if (countriesDone) "the countries you picked are on this phone" else "the countries you picked are still coming")
            used >= budget && fetched == 0 -> "the size you picked is already full (${used / 1_000_000} MB of tiles, most from browsing) , pick a bigger size to keep more"
            used >= budget -> "$fetched tiles this run; the size you picked is full"
            else -> "$fetched tiles this run around $from; everything near you is here" +
                (if (picked.isEmpty()) "" else if (countriesDone) ", and the countries you picked" else "; the countries you picked are still coming")
        })
        return Result.success()
    }

    companion object {
        const val RUN_MS = 8 * 60_000L // under WorkManager's ten minutes for a worker in the background
    }
}

/** Scheduling, the size on the phone, and the status line SETTINGS shows. */
object MapPrefetch {
    private const val PERIODIC = "maps-download"
    private const val NOW = "maps-download-now"
    private const val PREFS = "lg_maps"

    private fun constraints() = Constraints.Builder()
        .setRequiredNetworkType(NetworkType.UNMETERED) // Wi-Fi (or ethernet), never mobile data
        .setRequiresBatteryNotLow(true)
        .build()

    /** Once a day while the setting is on; KEEP, so calling it at every start changes nothing. */
    fun schedule(ctx: Context) {
        if (!AppSettings.mapDownload(ctx)) return
        val req = PeriodicWorkRequestBuilder<MapPrefetchWorker>(24, TimeUnit.HOURS)
            .setConstraints(constraints())
            .setBackoffCriteria(BackoffPolicy.LINEAR, 1, TimeUnit.MINUTES)
            .build()
        WorkManager.getInstance(ctx).enqueueUniquePeriodicWork(PERIODIC, ExistingPeriodicWorkPolicy.KEEP, req)
    }

    /**
     * [ download now ]: a run now, on Wi-Fi. It used to be KEEP with the daily run's constraints,
     * so a press did nothing while an earlier run sat waiting (a low battery, a backoff after a
     * partial run) and nothing on screen said so. Now a press replaces whatever was waiting, asks
     * only for Wi-Fi (the person asked for it, the battery is theirs to judge), and the status
     * line says "queued" until it starts, then counts the tiles as they land.
     */
    fun runNow(ctx: Context) {
        // the network you are on now: [ download now ] is a person's decision, and a hotel or a
        // phone's hotspot often counts as metered to Android, where UNMETERED never started and the
        // line said "starts on Wi-Fi" for ever
        val req = OneTimeWorkRequestBuilder<MapPrefetchWorker>()
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.LINEAR, 1, TimeUnit.MINUTES)
            .build()
        WorkManager.getInstance(ctx).enqueueUniqueWork(NOW, ExistingWorkPolicy.REPLACE, req)
        val cm = ctx.getSystemService(Context.CONNECTIVITY_SERVICE) as? android.net.ConnectivityManager
        val metered = cm?.isActiveNetworkMetered == true
        note(ctx, "queued", if (metered) "starting on this network (Android counts it as metered)" else "starting now")
    }

    /** What WorkManager says the [ download now ] run is doing: "waiting for a network", "running",
     *  or "" when nothing is queued , the status line's second opinion when the worker itself
     *  has not written a note yet. */
    fun runState(ctx: Context): String = try {
        val infos = WorkManager.getInstance(ctx).getWorkInfosForUniqueWork(NOW).get()
        when (infos.firstOrNull()?.state) {
            androidx.work.WorkInfo.State.ENQUEUED -> "waiting for a network"
            androidx.work.WorkInfo.State.RUNNING -> "running"
            androidx.work.WorkInfo.State.BLOCKED -> "waiting"
            else -> ""
        }
    } catch (_: Exception) { "" }


    fun cancel(ctx: Context) {
        WorkManager.getInstance(ctx).cancelUniqueWork(PERIODIC)
        WorkManager.getInstance(ctx).cancelUniqueWork(NOW)
    }

    internal fun tileFile(ctx: Context, t: MapPlan.Tile): File =
        if (t.kind == "land") File(File(ctx.filesDir, "landtiles"), "%03d_%03d.lgt".format(java.util.Locale.US, t.x, t.y))
        else File(File(ctx.filesDir, "roadtiles"), "%d_%04d_%04d.lgr".format(java.util.Locale.US, t.level, t.x, t.y))

    internal fun onPhone(ctx: Context, t: MapPlan.Tile): Boolean = tileFile(ctx, t).exists()

    /** Bytes of map tiles on the phone now (both caches, indexes included). */
    fun bytesOnPhone(ctx: Context): Long =
        listOf("landtiles", "roadtiles").sumOf { d -> File(ctx.filesDir, d).listFiles()?.sumOf { it.length() } ?: 0L }

    /** How much each tile cache may hold before the oldest go: the defaults, or the picked size
     *  when downloads are on (so the downloaded tiles are not trimmed away by browsing). */
    fun capBytes(ctx: Context, defaultBytes: Long): Long =
        if (AppSettings.mapDownload(ctx)) {
            // the picked countries sit on top of the size: they are not what the trim is for
            val countries = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getLong("countryBytes", 0)
            maxOf(defaultBytes, AppSettings.mapBudgetMB(ctx).toLong() * 1_000_000 + countries + 50_000_000)
        } else defaultBytes

    fun note(ctx: Context, state: String, why: String) {
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putString("state", state).putString("why", why).putLong("at", System.currentTimeMillis()).apply()
    }

    /** One picked country's progress, as the worker last saw it. */
    data class CountryProgress(val code: String, val name: String, val have: Int, val total: Int, val bytes: Long)

    fun noteCountry(ctx: Context, code: String, name: String, have: Int, total: Int, bytes: Long) {
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putString("country.$code", "$name|$have|$total|$bytes").apply()
    }

    fun noteCountryHave(ctx: Context, code: String, have: Int, total: Int) {
        val p = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val cur = (p.getString("country.$code", "") ?: "").split("|")
        if (cur.size < 4) return
        p.edit().putString("country.$code", "${cur[0]}|$have|$total|${cur[3]}").apply()
    }

    fun noteCountryBytes(ctx: Context, bytes: Long) {
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putLong("countryBytes", bytes).apply()
    }

    /** The picked countries' progress lines, in the order they were picked; a country the worker
     *  has not described yet has no line. */
    fun countryProgress(ctx: Context): List<CountryProgress> {
        val p = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        return AppSettings.mapCountries(ctx).mapNotNull { code ->
            val f = (p.getString("country.$code", "") ?: "").split("|")
            if (f.size < 4) null else CountryProgress(code, f[0], f[1].toIntOrNull() ?: 0, f[2].toIntOrNull() ?: 0, f[3].toLongOrNull() ?: 0)
        }
    }

    /** Forget a country's line when it is unpicked (its tiles stay until the cache trims them). */
    fun forgetCountry(ctx: Context, code: String) {
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().remove("country.$code").apply()
    }

    /** "312 MB on this phone · done 2 h ago: everything near you is here" */
    fun statusLine(ctx: Context): String {
        val p = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val mb = bytesOnPhone(ctx) / 1_000_000
        val at = p.getLong("at", 0)
        val state = p.getString("state", "") ?: ""
        val why = p.getString("why", "") ?: ""
        if (at == 0L) return "$mb MB of map on this phone · not downloaded yet (the daily run waits for Wi-Fi; [ download now ] uses this network)"
        val ago = (System.currentTimeMillis() - at) / 60_000
        val whenS = when { ago < 1 -> "just now"; ago < 60 -> "$ago min ago"; ago < 48 * 60 -> "${ago / 60} h ago"; else -> "${ago / 1440} days ago" }
        val live = if (state == "queued" && ago >= 1) runState(ctx).let { if (it.isEmpty()) "" else " · $it" } else ""
        return "$mb MB of map on this phone · $state $whenS" + (if (why.isNotEmpty()) ": $why" else "") + live
    }
}
