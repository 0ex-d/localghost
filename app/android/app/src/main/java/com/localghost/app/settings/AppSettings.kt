package com.localghost.app.settings

import android.content.Context

/**
 * Persisted user settings. Privacy-respecting defaults: sync is Wi-Fi only unless the
 * user explicitly opts into mobile data.
 */
object AppSettings {
    private const val PREFS = "lg_settings"
    private const val KEY_MOBILE_SYNC = "allow_mobile_sync"
    private const val KEY_ASKED_MEDIA = "ever_asked_media"
    private const val KEY_LAST_AUTOSYNC = "last_autosync_at"

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    /** Epoch millis of the last AUTOMATIC sync kick, so returning to the app does not restart sync. */
    fun lastAutoSyncAt(ctx: Context): Long = prefs(ctx).getLong(KEY_LAST_AUTOSYNC, 0L)
    fun setLastAutoSyncAt(ctx: Context, at: Long) =
        prefs(ctx).edit().putLong(KEY_LAST_AUTOSYNC, at).apply()

    /** false = Wi-Fi only (default). true = also sync on 4G/5G. */
    fun allowMobileSync(ctx: Context): Boolean = prefs(ctx).getBoolean(KEY_MOBILE_SYNC, false)
    fun setAllowMobileSync(ctx: Context, allow: Boolean) =
        prefs(ctx).edit().putBoolean(KEY_MOBILE_SYNC, allow).apply()

    /** Whether we've ever shown the media permission prompt — distinguishes never-asked
     *  (prompt still works) from permanently-denied (prompt dead, settings only). */
    fun everAskedMedia(ctx: Context): Boolean = prefs(ctx).getBoolean(KEY_ASKED_MEDIA, false)
    fun setEverAskedMedia(ctx: Context, asked: Boolean) =
        prefs(ctx).edit().putBoolean(KEY_ASKED_MEDIA, asked).apply()
}
