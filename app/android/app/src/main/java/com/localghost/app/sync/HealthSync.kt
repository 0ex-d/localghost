package com.localghost.app.sync

import android.content.Context
import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.permission.HealthPermission
import androidx.health.connect.client.records.DistanceRecord
import androidx.health.connect.client.records.ExerciseSessionRecord
import androidx.health.connect.client.records.FloorsClimbedRecord
import androidx.health.connect.client.records.HeartRateRecord
import androidx.health.connect.client.records.SleepSessionRecord
import androidx.health.connect.client.records.StepsRecord
import androidx.health.connect.client.records.TotalCaloriesBurnedRecord
import androidx.health.connect.client.records.WeightRecord
import androidx.health.connect.client.request.AggregateGroupByPeriodRequest
import androidx.health.connect.client.request.ReadRecordsRequest
import androidx.health.connect.client.time.TimeRangeFilter
import com.localghost.app.net.BoxClient
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * Reads the phone's Health Connect store , where Samsung Health (and every other health app that
 * plays fair) writes , and ships day-batches to the box: steps, sleep minutes, exercise minutes.
 * Health Connect is on-device; the ONLY network hop this data ever takes is phone -> box over the
 * paired channel, same as photos. The last 7 days each sync: metrics upsert on the box, so
 * refinement is free and re-sync is harmless.
 */
object HealthSync {
    val PERMISSIONS = setOf(
        HealthPermission.getReadPermission(StepsRecord::class),
        HealthPermission.getReadPermission(SleepSessionRecord::class),
        HealthPermission.getReadPermission(ExerciseSessionRecord::class),
        HealthPermission.getReadPermission(HeartRateRecord::class),
        HealthPermission.getReadPermission(DistanceRecord::class),
        HealthPermission.getReadPermission(TotalCaloriesBurnedRecord::class),
        HealthPermission.getReadPermission(FloorsClimbedRecord::class),
        HealthPermission.getReadPermission(WeightRecord::class),
    )

    fun available(ctx: Context): Boolean =
        HealthConnectClient.getSdkStatus(ctx) == HealthConnectClient.SDK_AVAILABLE

    /** How many of the read permissions are granted , the SYNC screen shows n/total. */
    suspend fun grantedCount(ctx: Context): Int = try {
        val granted = HealthConnectClient.getOrCreate(ctx).permissionController.getGrantedPermissions()
        PERMISSIONS.count { it in granted }
    } catch (_: Exception) { 0 }

    suspend fun hasPermissions(ctx: Context): Boolean = try {
        val granted = HealthConnectClient.getOrCreate(ctx).permissionController.getGrantedPermissions()
        granted.containsAll(PERMISSIONS)
    } catch (_: Exception) { false }

    data class SyncResult(val days: Int, val skipped: List<String>, val error: String? = null)

    /** FULL HISTORY , walks back month by month from now, syncing each window, until `emptyStop`
     *  consecutive empty months say the record ends (capped 20 years , if your watch predates
     *  that, congratulations). Each month is its own upload chunk, so memory and the box's 1MB
     *  cap stay honoured no matter how dense a life gets. onProgress gets a short status line. */
    suspend fun syncAll(ctx: Context, onProgress: (String) -> Unit): SyncResult {
        var months = 0
        var shipped = 0
        var empties = 0
        val allSkipped = LinkedHashSet<String>()
        val cal = java.util.Calendar.getInstance()
        while (months < 240 && empties < 6) {
            val end = cal.timeInMillis
            cal.add(java.util.Calendar.MONTH, -1)
            val start = cal.timeInMillis
            val fmt = java.text.SimpleDateFormat("MMM yyyy", java.util.Locale.US)
            onProgress("reading ${fmt.format(java.util.Date(start))}…")
            val r = sync(ctx, Instant.ofEpochMilli(start), Instant.ofEpochMilli(end))
            allSkipped.addAll(r.skipped)
            if (r.error != null && shipped == 0 && months == 0) return SyncResult(0, r.skipped, r.error)
            if (r.days == 0) empties++ else { empties = 0; shipped += r.days }
            months++
            onProgress("$shipped day(s) shipped · ${months} month(s) walked")
        }
        return SyncResult(shipped, allSkipped.toList())
    }

    /** Read one window and upload. EACH record type is isolated: a denied permission or a
     *  flaky provider skips that type (named in `skipped`) rather than failing the sync , partial
     *  data honestly labelled beats all-or-nothing. */
    suspend fun sync(ctx: Context, fromT: Instant? = null, toT: Instant? = null): SyncResult = try {
        val client = HealthConnectClient.getOrCreate(ctx)
        val zone = ZoneId.systemDefault()
        val fmt = DateTimeFormatter.ofPattern("yyyy-MM-dd")
        val now = toT ?: Instant.now()
        val from = fromT ?: now.minusSeconds(7L * 86400)
        val range = TimeRangeFilter.between(from, now)
        val days = HashMap<String, HashMap<String, Double>>()
        fun bucket(t: Instant): HashMap<String, Double> {
            val d = t.atZone(zone).toLocalDate().format(fmt)
            return days.getOrPut(d) { HashMap() }
        }
        val skipped = ArrayList<String>()
        suspend fun <T : Any> tryRead(name: String, cls: kotlin.reflect.KClass<T>, use: suspend (List<T>) -> Unit)
            where T : androidx.health.connect.client.records.Record {
            try {
                // PAGINATED , Health Connect returns ~1000 records a page; taking only page one
                // silently dropped everything past it. Loop the token until the store runs dry.
                var token: String? = null
                do {
                    val resp = client.readRecords(
                        if (token == null) ReadRecordsRequest(cls, range)
                        else ReadRecordsRequest(cls, range, pageToken = token))
                    use(resp.records)
                    token = resp.pageToken
                } while (!token.isNullOrEmpty())
            } catch (e: Exception) {
                skipped.add(name)
                android.util.Log.w("LocalGhost", "health read $name skipped: ${e.message}")
            }
        }
        // DAILY TOTALS VIA THE AGGREGATE API , the double-counting fix. When the watch AND the
        // phone both write steps, raw record-summing counts both; aggregate() dedupes across data
        // origins with Health Connect's own source-priority rules. One call covers steps,
        // distance, calories, floors and exercise duration, bucketed per local day. If aggregate
        // itself fails (older provider), the raw per-record fallback below still runs , counts
        // may inflate there, which the skipped-list names honestly.
        var aggregated = false
        try {
            val zStart = from.atZone(zone).toLocalDateTime()
            val zEnd = now.atZone(zone).toLocalDateTime()
            val buckets = client.aggregateGroupByPeriod(
                AggregateGroupByPeriodRequest(
                    metrics = setOf(
                        StepsRecord.COUNT_TOTAL,
                        DistanceRecord.DISTANCE_TOTAL,
                        TotalCaloriesBurnedRecord.ENERGY_TOTAL,
                        FloorsClimbedRecord.FLOORS_CLIMBED_TOTAL,
                        ExerciseSessionRecord.EXERCISE_DURATION_TOTAL,
                    ),
                    timeRangeFilter = TimeRangeFilter.between(zStart, zEnd),
                    timeRangeSlicer = java.time.Period.ofDays(1)))
            buckets.forEach { b ->
                // ZERO IS NOT DATA here , empty buckets return non-null zeros for some metric
                // types (Duration aggregates in particular), and writing them created 7,305
                // phantom "health days" back to 2006: every day had one zero metric, so the
                // six-empty-months stop never fired and the walk ran to its 20-year cap. A day
                // exists only when a POSITIVE value landed.
                val vals = HashMap<String, Double>()
                b.result[StepsRecord.COUNT_TOTAL]?.toDouble()?.takeIf { it > 0 }?.let { vals["steps"] = it }
                b.result[DistanceRecord.DISTANCE_TOTAL]?.inKilometers?.takeIf { it > 0 }?.let { vals["distance_km"] = it }
                b.result[TotalCaloriesBurnedRecord.ENERGY_TOTAL]?.inKilocalories?.takeIf { it > 0 }?.let { vals["calories"] = it }
                b.result[FloorsClimbedRecord.FLOORS_CLIMBED_TOTAL]?.takeIf { it > 0 }?.let { vals["floors"] = it }
                b.result[ExerciseSessionRecord.EXERCISE_DURATION_TOTAL]?.seconds?.takeIf { it > 0 }
                    ?.let { vals["exercise_minutes"] = it / 60.0 }
                if (vals.isNotEmpty()) {
                    days.getOrPut(b.startTime.toLocalDate().format(fmt)) { HashMap() }.putAll(vals)
                }
            }
            aggregated = true
        } catch (e: Exception) {
            android.util.Log.w("LocalGhost", "aggregate unavailable, raw fallback: ${e.message}")
        }
        if (!aggregated) tryRead("steps", StepsRecord::class) { recs ->
            recs.forEach { r ->
                val m = bucket(r.startTime)
                m["steps"] = (m["steps"] ?: 0.0) + r.count.toDouble()
            }
        }
        tryRead("sleep", SleepSessionRecord::class) { recs ->
            // OVERLAP MERGE , two origins (watch app + phone app) can record the SAME night as two
            // overlapping sessions; naive summing invents extra sleep. Merge intervals first, then
            // bucket each merged block by its END (a night belongs to the day you wake).
            val ivs = recs.map { it.startTime.epochSecond to it.endTime.epochSecond }
                .filter { it.second > it.first }.sortedBy { it.first }
            val merged = ArrayList<Pair<Long, Long>>()
            for (iv in ivs) {
                val last = merged.lastOrNull()
                if (last != null && iv.first <= last.second) {
                    merged[merged.size - 1] = last.first to maxOf(last.second, iv.second)
                } else merged.add(iv)
            }
            merged.forEach { (st, en) ->
                val m = bucket(Instant.ofEpochSecond(en))
                m["sleep_minutes"] = (m["sleep_minutes"] ?: 0.0) + (en - st) / 60.0
            }
        }
        if (!aggregated) tryRead("exercise", ExerciseSessionRecord::class) { recs ->
            recs.forEach { r ->
                val m = bucket(r.startTime)
                m["exercise_minutes"] = (m["exercise_minutes"] ?: 0.0) +
                    (r.endTime.epochSecond - r.startTime.epochSecond) / 60.0
            }
        }
        // Heart rate: DAILY avg/min/max into metrics, plus the raw series THINNED to 5-minute
        // buckets as samples , a watch-day is ~1440 readings, thinning keeps a week's upload at a
        // few thousand points while preserving the shape of the day.
        val hrSamples = ArrayList<Triple<String, Long, Double>>()
        val hrByDay = HashMap<String, MutableList<Double>>()
        var lastBucket = 0L
        tryRead("heart rate", HeartRateRecord::class) { recs ->
          recs.forEach { r ->
            r.samples.forEach { smp ->
                val d = smp.time.atZone(zone).toLocalDate().format(fmt)
                hrByDay.getOrPut(d) { ArrayList() }.add(smp.beatsPerMinute.toDouble())
                val bucket = smp.time.epochSecond / 300 * 300
                if (bucket != lastBucket) {
                    lastBucket = bucket
                    hrSamples.add(Triple("heart_rate", bucket, smp.beatsPerMinute.toDouble()))
                }
            }
          }
        }
        hrByDay.forEach { (d, vals) ->
            if (vals.isNotEmpty()) {
                val m = days.getOrPut(d) { HashMap() }
                m["hr_avg"] = vals.average()
                m["hr_min"] = vals.min()
                m["hr_max"] = vals.max()
            }
        }
        if (!aggregated) tryRead("distance", DistanceRecord::class) { recs ->
            recs.forEach { r ->
                val m = bucket(r.startTime)
                m["distance_km"] = (m["distance_km"] ?: 0.0) + r.distance.inKilometers
            }
        }
        if (!aggregated) tryRead("calories", TotalCaloriesBurnedRecord::class) { recs ->
            recs.forEach { r ->
                val m = bucket(r.startTime)
                m["calories"] = (m["calories"] ?: 0.0) + r.energy.inKilocalories
            }
        }
        if (!aggregated) tryRead("floors", FloorsClimbedRecord::class) { recs ->
            recs.forEach { r ->
                val m = bucket(r.startTime)
                m["floors"] = (m["floors"] ?: 0.0) + r.floors
            }
        }
        tryRead("weight", WeightRecord::class) { recs ->
            recs.forEach { r -> bucket(r.time)["weight_kg"] = r.weight.inKilograms }
        }
        // A CONSTANT IS NOT A MEASUREMENT. Samsung Health synthesizes a BMR-based calories total
        // for ANY day you query , 1564.50 kcal, every day since 2006, data or no data ("you were
        // presumably alive"). One constant made every day in history look like a health day and
        // marched the full-history walk to its 20-year cap. Rule: calories only count on days
        // where something was actually MEASURED (any other metric present); a day whose only
        // content is the provider's guess about your resting metabolism is an empty day.
        days.entries.removeAll { (_, m) -> m.keys.all { it == "calories" } }
        when {
            days.isEmpty() && hrSamples.isEmpty() ->
                SyncResult(0, skipped, if (skipped.size >= 8) "every record type failed , re-check permissions" else null)
            BoxClient.healthUpload(ctx, days, hrSamples) -> SyncResult(days.size, skipped)
            else -> SyncResult(0, skipped, "box unreachable , is it unlocked?")
        }
    } catch (e: Exception) {
        android.util.Log.w("LocalGhost", "health sync: ${e.message}")
        SyncResult(0, emptyList(), e.message ?: "health sync failed")
    }
}
