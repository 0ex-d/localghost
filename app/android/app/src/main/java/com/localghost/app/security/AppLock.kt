package com.localghost.app.security

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

/**
 * Local convenience lock ONLY. The real security boundary is cert + box, not this.
 * Its job: stop a grabbed, unlocked phone from firing PINs at the box without a live
 * biometric first. It seals no local vault — there isn't one. The key is auth-gated
 * and used purely to prove a fresh biometric/credential auth happened.
 */
object AppLock {
    private const val ALIAS = "lg_gate_key"
    private const val KS = "AndroidKeyStore"

    private fun ks() = KeyStore.getInstance(KS).apply { load(null) }

    fun ensureKey() {
        if (ks().containsAlias(ALIAS)) return
        try { generate(true) } catch (e: StrongBoxUnavailableException) { generate(false) }
    }

    private fun generate(strongBox: Boolean) {
        val spec = KeyGenParameterSpec.Builder(
            ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setKeySize(256)
            .setUserAuthenticationRequired(true)
            .setUserAuthenticationParameters(0,
                KeyProperties.AUTH_BIOMETRIC_STRONG or KeyProperties.AUTH_DEVICE_CREDENTIAL)
            .setIsStrongBoxBacked(strongBox)
            .build()
        KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KS)
            .apply { init(spec) }.generateKey()
    }

    private fun key(): SecretKey =
        (ks().getEntry(ALIAS, null) as KeyStore.SecretKeyEntry).secretKey

    fun gateCipher(): Cipher =
        Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key()) }
}
