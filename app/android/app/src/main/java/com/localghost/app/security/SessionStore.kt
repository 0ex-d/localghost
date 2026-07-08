package com.localghost.app.security

import android.content.Context
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec

/**
 * Encrypted store for the SESSION token minted at PIN unlock (distinct from BoxConfig's enrolment
 * token, which is the device identity). The session token authorises foreground requests AND the
 * background notification poller; the box issues a fresh one on every unlock (mount or warm) with a
 * 2-day lifetime.
 *
 * Why persist it: most opens do NOT re-mount (the box stays mounted across reboots of the app), so
 * the app carries the last-issued token and its expiry. The background poller uses it to fetch
 * notifications without a re-unlock. Once it expires the box can no longer authenticate the poller,
 * so notifications stop until the user re-opens and re-unlocks , the app reads expiresAt here to show
 * that "reopen to check notifications" state BEFORE the token is dead, rather than silently going
 * quiet.
 *
 * Stored in private SharedPreferences, AES-GCM under an AndroidKeyStore key (same scheme as
 * BoxConfig). The token is high-entropy and short-lived; this is defence in depth, not the only
 * guard (the mTLS client cert already gates the channel).
 */
object SessionStore {
    private const val PREFS = "lg_session"
    private const val KEY_ALIAS = "lg_session_key"
    private const val TRANSFORM = "AES/GCM/NoPadding"
    private const val K_TOKEN = "token"
    private const val K_EXPIRES = "expiresAtEpochSec"

    data class Session(val token: String, val expiresAtEpochSec: Long)

    /** The current session, or null if none stored. */
    fun read(ctx: Context): Session? {
        val p = prefs(ctx)
        val token = decrypt(p.getString(K_TOKEN, null)) ?: return null
        val exp = p.getLong(K_EXPIRES, 0L)
        if (token.isBlank() || exp == 0L) return null
        return Session(token, exp)
    }

    /** Persist a freshly-issued token + its expiry (RFC3339 UTC from the box, parsed to epoch sec). */
    fun write(ctx: Context, token: String, expiresAtEpochSec: Long) {
        prefs(ctx).edit()
            .putString(K_TOKEN, encrypt(token))
            .putLong(K_EXPIRES, expiresAtEpochSec)
            .apply()
    }

    /** Drop the session (on lock, un-enroll, or an observed 503 that means the token is dead). */
    fun clear(ctx: Context) {
        prefs(ctx).edit().clear().apply()
    }

    /** True if there is no token or it has already expired , the app must prompt a re-unlock. */
    fun isExpired(ctx: Context, nowEpochSec: Long): Boolean {
        val s = read(ctx) ?: return true
        return nowEpochSec >= s.expiresAtEpochSec
    }

    /**
     * True if the token is still valid but within `windowSec` of expiring , the cue to surface the
     * gentle "you'll have to open the app to check for notifications" hint before it actually dies.
     */
    fun isExpiringSoon(ctx: Context, nowEpochSec: Long, windowSec: Long): Boolean {
        val s = read(ctx) ?: return false
        val remaining = s.expiresAtEpochSec - nowEpochSec
        return remaining in 0 until windowSec
    }

    // --- crypto (mirrors BoxConfig: AES-GCM, AndroidKeyStore-held key) ---

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    private fun encrypt(plain: String): String {
        val cipher = Cipher.getInstance(TRANSFORM).apply { init(Cipher.ENCRYPT_MODE, key()) }
        val iv = cipher.iv
        val ct = cipher.doFinal(plain.toByteArray(Charsets.UTF_8))
        return Base64.encodeToString(iv + ct, Base64.NO_WRAP)
    }

    private fun decrypt(stored: String?): String? {
        if (stored == null) return null
        return try {
            val blob = Base64.decode(stored, Base64.NO_WRAP)
            val iv = blob.copyOfRange(0, 12)
            val ct = blob.copyOfRange(12, blob.size)
            val cipher = Cipher.getInstance(TRANSFORM).apply {
                init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, iv))
            }
            String(cipher.doFinal(ct), Charsets.UTF_8)
        } catch (e: Exception) {
            null // unreadable (e.g. key rotated) reads as "no session", which forces a re-unlock
        }
    }

    private fun key(): javax.crypto.SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getEntry(KEY_ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }
        // create on first use
        val kg = javax.crypto.KeyGenerator.getInstance(
            android.security.keystore.KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore"
        )
        kg.init(
            android.security.keystore.KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                android.security.keystore.KeyProperties.PURPOSE_ENCRYPT or
                    android.security.keystore.KeyProperties.PURPOSE_DECRYPT
            )
                .setBlockModes(android.security.keystore.KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(android.security.keystore.KeyProperties.ENCRYPTION_PADDING_NONE)
                .build()
        )
        return kg.generateKey()
    }
}
