package com.localghost.app.security

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

/**
 * Biometric gate key. A keystore AES key bound to user authentication — the biometric
 * prompt unlocks a CryptoObject around this cipher, proving a real fingerprint/face, not
 * just a tapped button.
 */
object AppLock {
    private const val ALIAS = "localghost.gate"
    private const val TRANSFORM = "AES/GCM/NoPadding"

    fun ensureKey() {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        if (ks.containsAlias(ALIAS)) return
        generate()
    }

    private fun generate() {
        val spec = KeyGenParameterSpec.Builder(
            ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT
        )
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setUserAuthenticationRequired(true)
            .setUserAuthenticationParameters(
                0, KeyProperties.AUTH_BIOMETRIC_STRONG or KeyProperties.AUTH_DEVICE_CREDENTIAL)
            .build()
        KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
            .apply { init(spec) }.generateKey()
    }

    fun gateCipher(): Cipher {
        // Auth-bound keystore keys are PERMANENTLY invalidated when the user adds or removes a
        // fingerprint (or changes the device credential). containsAlias stays true for an invalidated
        // key, so ensureKey() never notices; the failure surfaces here, at cipher init, as
        // KeyPermanentlyInvalidatedException , previously an uncaught crash at the lock screen on
        // EVERY launch until app data was cleared. Recovery is safe and correct: this key only gates
        // the phone UI (the box PIN is the real credential, and the box never trusts this key), so we
        // delete the dead key, mint a fresh one bound to the CURRENT biometric set, and retry once.
        return try {
            initCipher()
        } catch (e: java.security.InvalidKeyException) {
            val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
            try { ks.deleteEntry(ALIAS) } catch (_: Exception) { /* already gone is fine */ }
            generate()
            initCipher()
        }
    }

    private fun initCipher(): Cipher {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        val key = ks.getKey(ALIAS, null) as? SecretKey
            ?: throw java.security.InvalidKeyException("gate key missing")
        return Cipher.getInstance(TRANSFORM).apply { init(Cipher.ENCRYPT_MODE, key) }
    }
}
