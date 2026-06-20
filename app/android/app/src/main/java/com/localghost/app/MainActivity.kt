package com.localghost.app

import android.hardware.biometrics.BiometricManager.Authenticators.BIOMETRIC_STRONG
import android.hardware.biometrics.BiometricManager.Authenticators.DEVICE_CREDENTIAL
import android.hardware.biometrics.BiometricPrompt
import android.os.Bundle
import android.os.CancellationSignal
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.lifecycleScope
import com.localghost.app.net.BoxClient
import com.localghost.app.security.AppLock
import com.localghost.app.security.DeviceIdentity
import com.localghost.app.ui.HomeScreen
import com.localghost.app.ui.LockScreen
import com.localghost.app.ui.PinScreen
import com.localghost.app.ui.theme.LocalGhostTheme
import kotlinx.coroutines.launch

private sealed interface Screen {
    data object Gate : Screen        // biometric convenience lock
    data object Pin : Screen         // variable-length code entry
    data object Home : Screen        // mounted session (whichever persona the box gave us)
}

class MainActivity : ComponentActivity() {

    private var screen by mutableStateOf<Screen>(Screen.Gate)
    private var busy by mutableStateOf(false)
    private var error by mutableStateOf<String?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        DeviceIdentity.ensureKey()   // cert identity: the real authentication
        AppLock.ensureKey()          // local biometric gate only

        enableEdgeToEdge()
        setContent {
            LocalGhostTheme {
                when (screen) {
                    Screen.Gate -> LockScreen(error) { passBiometric() }
                    Screen.Pin  -> PinScreen(busy, error) { submit(it) }
                    Screen.Home -> HomeScreen()
                }
            }
        }
    }

    override fun onStop() {
        super.onStop()
        // Dropping out of foreground returns to the biometric gate. Note: the BOX
        // keeps the persona mounted across this — backgrounding the phone does not
        // unmount. That's deliberate: the box must keep running to push messages.
        screen = Screen.Gate
        busy = false
        error = null
    }

    private fun passBiometric() {
        error = null
        BiometricPrompt.Builder(this)
            .setTitle("Unlock LocalGhost")
            .setSubtitle("Authenticate to enter your code")
            .setAllowedAuthenticators(BIOMETRIC_STRONG or DEVICE_CREDENTIAL)
            .build()
            .authenticate(BiometricPrompt.CryptoObject(AppLock.gateCipher()),
                CancellationSignal(), mainExecutor,
                object : BiometricPrompt.AuthenticationCallback() {
                    override fun onAuthenticationSucceeded(r: BiometricPrompt.AuthenticationResult) {
                        screen = Screen.Pin
                    }
                    override fun onAuthenticationError(code: Int, msg: CharSequence) {
                        error = msg.toString()
                    }
                })
    }

    private fun submit(pin: String) {
        busy = true; error = null
        lifecycleScope.launch {
            // Sent over mTLS to ghost.secd (stubbed). The box decides everything;
            // we just wait the uniform window and render whatever session we get.
            val session = BoxClient.submitPin(pin)
            busy = false
            if (session.ok) screen = Screen.Home
            else error = "Could not reach your box"   // network failure only, never "wrong PIN"
        }
    }
}
