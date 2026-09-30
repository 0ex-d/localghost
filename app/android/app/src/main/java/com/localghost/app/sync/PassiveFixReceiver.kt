package com.localghost.app.sync

import android.Manifest
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationManager
import android.location.LocationRequest
import android.os.Build
import androidx.core.content.ContextCompat

/**
 * THE PASSIVE FIXES. The quarter-hour worker is the trail's ruling: no service, no GPS wake-ups
 * of our own. The passive provider costs nothing on top of that: whenever ANY app on the phone
 * asks for a position , Maps open for directions, the camera geotagging a photo, a weather app ,
 * the OS hands us a copy. Delivered by PendingIntent to this receiver, so it works with the app's
 * process dead; the OS starts us for the moment it takes to write a line. Every copy goes through
 * LocationLog.record with the same rules as the worker's fixes (25 m or an hour, coarse fixes are
 * confirmations not moves, timestamps strictly increasing), so the trail gets denser only while
 * the phone is genuinely moving with someone else's GPS on, which is exactly when the day route
 * wants the detail. Registered with the worker (LocationLog.schedule), gone with it
 * (LocationLog.stop), re-registered at boot like the worker (PendingIntent requests do not survive
 * a reboot). Needs the background location permission the trail already asks for; without it the
 * request is simply not made.
 */
class PassiveFixReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (!LocationLog.active(context)) return
        val locs = ArrayList<Location>()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            @Suppress("DEPRECATION")
            (intent.getParcelableArrayListExtra<Location>(LocationManager.KEY_LOCATIONS))?.let { locs.addAll(it) }
        }
        if (locs.isEmpty()) {
            @Suppress("DEPRECATION")
            (intent.getParcelableExtra<Location>(LocationManager.KEY_LOCATION_CHANGED))?.let { locs.add(it) }
        }
        var kept = 0
        for (l in locs.sortedBy { it.time }) {
            if (l.time <= 0L || (l.latitude == 0.0 && l.longitude == 0.0)) continue
            // a mock fix (a developer's fake GPS app) is not where the phone is
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && l.isMock) continue
            val acc = if (l.hasAccuracy()) l.accuracy else 0f
            if (LocationLog.record(context, LocationLog.Point(l.time / 1000, l.latitude, l.longitude, acc, LocationLog.VIA_PASSIVE))) kept++
        }
        if (kept > 0) LocationLog.notePassive(context, kept)
    }

    companion object {
        private const val MIN_TIME_MS = 60_000L
        private const val MIN_DISTANCE_M = 25f

        private fun pending(ctx: Context): PendingIntent {
            val flags = PendingIntent.FLAG_UPDATE_CURRENT or
                (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0)
            return PendingIntent.getBroadcast(ctx, 7, Intent(ctx, PassiveFixReceiver::class.java), flags)
        }

        /** Ask the OS for copies of other apps' fixes. Silent no-op without the permissions or the provider. */
        fun register(ctx: Context) {
            val lm = ctx.getSystemService(Context.LOCATION_SERVICE) as? LocationManager ?: return
            val fine = ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
            val bg = Build.VERSION.SDK_INT < Build.VERSION_CODES.Q ||
                ContextCompat.checkSelfPermission(ctx, Manifest.permission.ACCESS_BACKGROUND_LOCATION) == PackageManager.PERMISSION_GRANTED
            if (!fine || !bg || !lm.allProviders.contains(LocationManager.PASSIVE_PROVIDER)) return
            try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                    val req = LocationRequest.Builder(MIN_TIME_MS)
                        .setMinUpdateDistanceMeters(MIN_DISTANCE_M)
                        .setQuality(LocationRequest.QUALITY_LOW_POWER)
                        .build()
                    lm.requestLocationUpdates(LocationManager.PASSIVE_PROVIDER, req, pending(ctx))
                } else {
                    @Suppress("DEPRECATION")
                    lm.requestLocationUpdates(LocationManager.PASSIVE_PROVIDER, MIN_TIME_MS, MIN_DISTANCE_M, pending(ctx))
                }
            } catch (e: Exception) {
                android.util.Log.w("LocalGhost", "passive fixes: ${e.message}")
            }
        }

        fun unregister(ctx: Context) {
            val lm = ctx.getSystemService(Context.LOCATION_SERVICE) as? LocationManager ?: return
            try { lm.removeUpdates(pending(ctx)) } catch (_: Exception) { }
        }
    }
}
