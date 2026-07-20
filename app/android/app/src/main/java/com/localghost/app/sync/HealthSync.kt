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

    /** Read the last 7 days and upload. EACH record type is isolated: a denied permission or a
     *  flaky provider skips that type (named in `skipped`) rather than failing the sync , partial
     *  data honestly labelled beats all-or-nothing. */
    suspend fun sync(ctx: Context): SyncResult = try {
        val client = HealthConnectClient.getOrCreate(ctx)
        val zone = ZoneId.systemDefault()
        val fmt = DateTimeFormatter.ofPattern("yyyy-MM-dd")
        val now = Instant.now()
        val from = now.minusSeconds(7L * 86400)
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
                use(client.readRecords(ReadRecordsRequest(cls, range)).records)
            } catch (e: Exception) {
                skipped.add(name)
                android.util.Log.w("LocalGhost", "health read $name skipped: ${e.message}")
            }
        }
        tryRead("steps", StepsRecord::class) { recs ->
            recs.forEach { r ->
                val m = bucket(r.startTime)
                m["steps"] = (m["steps"] ?: 0.0) + r.count.toDouble()
            }
        }
        tryRead("sleep", SleepSessionRecord::class) { recs ->
            recs.forEach { r ->
                // A night's sleep belongs to the day you WAKE , bucket by end time.
                val m = bucket(r.endTime)
                m["sleep_minutes"] = (m["sleep_minutes"] ?: 0.0) +
                    (r.endTime.epochSecond - r.startTime.epochSecond) / 60.0
            }
        }
        tryRead("exercise", ExerciseSessionRecord::class) { recs ->
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
        tryRead("distance", DistanceRecord::class) { recs ->
            recs.forEach { r ->
                val m = bucket(r.startTime)
                m["distance_km"] = (m["distance_km"] ?: 0.0) + r.distance.inKilometers
            }
        }
        tryRead("calories", TotalCaloriesBurnedRecord::class) { recs ->
            recs.forEach { r ->
                val m = bucket(r.startTime)
                m["calories"] = (m["calories"] ?: 0.0) + r.energy.inKilocalories
            }
        }
        tryRead("floors", FloorsClimbedRecord::class) { recs ->
            recs.forEach { r ->
                val m = bucket(r.startTime)
                m["floors"] = (m["floors"] ?: 0.0) + r.floors
            }
        }
        tryRead("weight", WeightRecord::class) { recs ->
            recs.forEach { r -> bucket(r.time)["weight_kg"] = r.weight.inKilograms }
        }
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
