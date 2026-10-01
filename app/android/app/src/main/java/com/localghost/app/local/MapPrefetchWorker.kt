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
        if (centers.isEmpty()) {
            MapPrefetch.note(ctx, "idle", "nothing to go on yet: no fix on this phone and no trail on the box , the outlines are here, tiles follow once the box knows where you have been")
            return Result.success()
        }
        val plan = MapPlan.plan(centers, land, roads)
        var used = MapPrefetch.bytesOnPhone(ctx)
        var fetched = 0
        var failedInARow = 0
        for (t in plan) {
            if (isStopped) return Result.retry()
            if (used >= budget) break
            if (MapPrefetch.onPhone(ctx, t)) continue
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
            // SETTINGS shows this live while it runs
            if (fetched % 10 == 0) MapPrefetch.note(ctx, "running", "$fetched tiles so far this run")
        }
        MapPrefetch.note(ctx, "done", when {
            used >= budget && fetched == 0 -> "the size you picked is already full (${used / 1_000_000} MB of tiles, most from browsing) , pick a bigger size to keep more"
            used >= budget -> "$fetched tiles this run; the size you picked is full"
            else -> "$fetched tiles this run around $from; everything near you is here"
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
        if (AppSettings.mapDownload(ctx)) maxOf(defaultBytes, AppSettings.mapBudgetMB(ctx).toLong() * 1_000_000 + 50_000_000) else defaultBytes

    fun note(ctx: Context, state: String, why: String) {
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putString("state", state).putString("why", why).putLong("at", System.currentTimeMillis()).apply()
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
