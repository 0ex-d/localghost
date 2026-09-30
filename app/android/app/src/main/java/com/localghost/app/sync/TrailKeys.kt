package com.localghost.app.sync

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import com.localghost.app.net.BoxClient
import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.spec.MGF1ParameterSpec
import java.security.spec.X509EncodedKeySpec
import javax.crypto.Cipher
import javax.crypto.spec.OAEPParameterSpec
import javax.crypto.spec.PSource

/**
 * WHO HOLDS THE KEY TO THE TRAIL. The phone seals every point to a public key ([TrailSeal]); this
 * says where the private half is, and holds it in memory while (and only while) the app is
 * unlocked.
 *
 *   box    , the box's vault holds it (secd, /v1/trail/key); the phone keeps the public half only.
 *            A PIN unlock fetches the private half; locking the app forgets it. Without the box PIN
 *            the phone's trail does not open, not even in the app's local-only mode.
 *   phone  , a phone with no box yet: the private half is kept here, wrapped by an AndroidKeyStore
 *            RSA key that opens only within 30 s of the phone's own unlock (fingerprint, face or
 *            the phone's PIN). Wrapping needs only the RSA public key, so the key is made the first
 *            time a point is recorded, even in the background, and nothing is written in the clear.
 *            The first PIN unlock of a box hands the key over and deletes the phone's copy.
 *
 * Every point written before this existed (plain lines) is sealed the first time the key is here.
 */
object TrailKeys {
    private const val PREFS = "lg_trail_key"
    private const val WRAP_ALIAS = "localghost.trail.wrap.v1"
    private const val AUTH_WINDOW_S = 30
    private val OAEP = OAEPParameterSpec("SHA-256", "MGF1", MGF1ParameterSpec.SHA1, PSource.PSpecified.DEFAULT)

    @Volatile private var priv: ByteArray? = null

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    /** The public half points are sealed to, null before there is one. */
    fun publicKey(ctx: Context): ByteArray? {
        val s = prefs(ctx).getString("pub", null) ?: return null
        val b = TrailSeal.unb64(s) ?: return null
        return if (b.size == 32) b else null
    }

    /** "box", "phone", or "" before a key exists. */
    fun where(ctx: Context): String = prefs(ctx).getString("where", "") ?: ""

    /** Opens sealed lines while the app is unlocked; null while it is locked. */
    fun opener(): TrailSeal.Opener? = priv?.let { TrailSeal.Opener(it) }

    fun isOpen(): Boolean = priv != null

    /** The app locked: the private half leaves memory. */
    fun forget() {
        priv?.fill(0)
        priv = null
    }

    /**
     * A public key to seal to, made here the first time (see "phone" above). Null only when the
     * Keystore refuses, and then the caller writes the point in the clear rather than lose it; it
     * is sealed with the rest when a key exists.
     */
    @Synchronized
    fun ensure(ctx: Context): ByteArray? {
        publicKey(ctx)?.let { return it }
        return try {
            val k = TrailSeal.generate()
            val wrapped = wrap(k.priv)
            prefs(ctx).edit().putString("pub", TrailSeal.b64(k.pub)).putString("wrapped", TrailSeal.b64(wrapped))
                .putString("where", "phone").commit()
            k.priv.fill(0) // made in the background, perhaps: nothing here may read with it
            k.pub
        } catch (e: Exception) {
            android.util.Log.w("LocalGhost", "trail key not made: ${e.javaClass.simpleName} ${e.message}")
            null
        }
    }

    /**
     * The phone was just unlocked by its owner (the app's gate): a key kept on the phone opens into
     * memory. False when there is none here, or the unlock is too old for the Keystore.
     */
    fun onDeviceAuth(ctx: Context): Boolean {
        if (where(ctx) != "phone") return false
        val p = unwrap(ctx) ?: return false
        priv = p
        LocationLog.sealPlainLines(ctx)
        return true
    }

    /**
     * After a PIN unlock (there is a box session): the key is the box's. Fetches it into memory; a
     * key still kept on the phone is handed to the box first and the phone's copy deleted; a phone
     * with no key makes one and hands it over. False when the box could not be asked (the next
     * unlock tries again; points go on being sealed to the public half meanwhile).
     */
    suspend fun onBoxUnlocked(ctx: Context): Boolean {
        val ok = when (where(ctx)) {
            "box" -> fetch(ctx)
            "phone" -> handOver(ctx)
            else -> {
                val k = TrailSeal.generate()
                if (BoxClient.trailKeyPut(ctx, k.pub, k.priv)) {
                    prefs(ctx).edit().putString("pub", TrailSeal.b64(k.pub)).remove("wrapped").putString("where", "box").commit()
                    priv = k.priv
                    true
                } else false
            }
        }
        if (ok) LocationLog.sealPlainLines(ctx)
        return ok
    }

    private suspend fun fetch(ctx: Context): Boolean {
        val a = BoxClient.trailKeyGet(ctx) ?: return false
        val mine = publicKey(ctx)
        if (a.have && a.priv != null && a.pub != null) {
            if (mine != null && !mine.contentEquals(a.pub)) {
                // the box holds another key for this phone (a reinstall); from now on, its key. What
                // was sealed to the old one here cannot be opened by anyone and is dropped as it is met.
                android.util.Log.w("LocalGhost", "trail key: the box holds another key for this phone; taking the box's")
                prefs(ctx).edit().putString("pub", TrailSeal.b64(a.pub)).commit()
            }
            priv = a.priv
            return true
        }
        // the box has no key for this phone (a new box, or this phone's certificate changed and the
        // key did not follow): a new one, so points from now on reach it
        android.util.Log.w("LocalGhost", "trail key: the box has none for this phone; making a new one")
        val k = TrailSeal.generate()
        if (!BoxClient.trailKeyPut(ctx, k.pub, k.priv)) return false
        prefs(ctx).edit().putString("pub", TrailSeal.b64(k.pub)).putString("where", "box").commit()
        priv = k.priv
        return true
    }

    private suspend fun handOver(ctx: Context): Boolean {
        val pub = publicKey(ctx) ?: return false
        val p = priv ?: unwrap(ctx) ?: return false // the device unlock is too old: at the next one
        if (!BoxClient.trailKeyPut(ctx, pub, p)) return false
        prefs(ctx).edit().remove("wrapped").putString("where", "box").commit()
        try {
            KeyStore.getInstance("AndroidKeyStore").apply { load(null) }.deleteEntry(WRAP_ALIAS)
        } catch (_: Exception) { /* gone is fine */ }
        priv = p
        return true
    }

    // --- the phone's own wrap (a phone with no box) ---

    private fun wrapKey(): java.security.PublicKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        if (!ks.containsAlias(WRAP_ALIAS)) {
            val spec = KeyGenParameterSpec.Builder(WRAP_ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setKeySize(2048)
                .setDigests(KeyProperties.DIGEST_SHA256)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_RSA_OAEP)
                .setUserAuthenticationRequired(true)
                .setUserAuthenticationParameters(AUTH_WINDOW_S, KeyProperties.AUTH_BIOMETRIC_STRONG or KeyProperties.AUTH_DEVICE_CREDENTIAL)
                .build()
            KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_RSA, "AndroidKeyStore").apply { initialize(spec) }.generateKeyPair()
        }
        // a plain copy of the public key: encrypting needs no unlock, only opening does
        val pub = ks.getCertificate(WRAP_ALIAS).publicKey
        return KeyFactory.getInstance(pub.algorithm).generatePublic(X509EncodedKeySpec(pub.encoded))
    }

    private fun wrap(raw: ByteArray): ByteArray {
        val c = Cipher.getInstance("RSA/ECB/OAEPWithSHA-256AndMGF1Padding")
        c.init(Cipher.ENCRYPT_MODE, wrapKey(), OAEP)
        return c.doFinal(raw)
    }

    private fun unwrap(ctx: Context): ByteArray? {
        val wrapped = prefs(ctx).getString("wrapped", null)?.let { TrailSeal.unb64(it) } ?: return null
        return try {
            val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
            val key = ks.getKey(WRAP_ALIAS, null) as? PrivateKey ?: return null
            val c = Cipher.getInstance("RSA/ECB/OAEPWithSHA-256AndMGF1Padding")
            c.init(Cipher.DECRYPT_MODE, key, OAEP)
            c.doFinal(wrapped).takeIf { it.size == 32 }
        } catch (e: android.security.keystore.UserNotAuthenticatedException) {
            null // outside the window: the caller asks for an unlock, or waits for the next one
        } catch (e: Exception) {
            android.util.Log.w("LocalGhost", "trail key did not open: ${e.javaClass.simpleName}")
            null
        }
    }
}
