package com.localghost.app.security

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.security.KeyStore
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Short strings sealed to THIS phone's hardware (an AndroidKeyStore AES key that never leaves the
 * secure element and needs no unlock): what the background has to read while the app is locked,
 * the trail's last point above all (the worker compares every new fix with it). A copy of the
 * app's files taken off the phone does not open; code running as the app on the phone does. The
 * trail itself is sealed more strongly ([com.localghost.app.sync.TrailSeal]): only a PIN unlock
 * opens it.
 */
object DeviceSealed {
    private const val ALIAS = "localghost.device.v1"

    private fun key(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getKey(ALIAS, null) as? SecretKey)?.let { return it }
        val spec = KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setKeySize(256)
            .build()
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply { init(spec) }.generateKey()
    }

    /** [plain] sealed, as base64 (the IV first), or null when the Keystore would not. */
    fun seal(plain: String): String? = try {
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.ENCRYPT_MODE, key())
        Base64.getEncoder().encodeToString(c.iv + c.doFinal(plain.toByteArray(Charsets.UTF_8)))
    } catch (e: Exception) {
        android.util.Log.w("LocalGhost", "device seal failed: ${e.javaClass.simpleName}"); null
    }

    fun open(sealed: String?): String? {
        if (sealed.isNullOrEmpty()) return null
        return try {
            val raw = Base64.getDecoder().decode(sealed)
            val c = Cipher.getInstance("AES/GCM/NoPadding")
            c.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, raw, 0, 12))
            String(c.doFinal(raw, 12, raw.size - 12), Charsets.UTF_8)
        } catch (e: Exception) {
            null
        }
    }
}
