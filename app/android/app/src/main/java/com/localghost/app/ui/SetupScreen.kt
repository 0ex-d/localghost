package com.localghost.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.localghost.app.ui.theme.*

/**
 * First-run enrollment. Collects the box address, a one-time pairing code, and a device name,
 * then enrolls against the box. On success the caller persists the returned token via BoxConfig
 * and moves to the lock gate. The box address can be typed or filled from a scanned QR.
 *
 * onScanQr is invoked when the user taps SCAN; the host wires it to the scanner and calls back
 * with the decoded box URL (and optionally a code). Until the scanner is built, the typed path is
 * fully functional.
 */
@Composable
fun SetupScreen(
    busy: Boolean,
    error: String?,
    prefilledUrl: String = "",
    prefilledCode: String = "",
    onScanQr: () -> Unit,
    onEnroll: (url: String, code: String, deviceName: String, fingerprint: String) -> Unit,
    prefilledFingerprint: String = "",
) {
    var url by remember(prefilledUrl) { mutableStateOf(prefilledUrl) }
    var code by remember(prefilledCode) { mutableStateOf(prefilledCode) }
    var name by remember { mutableStateOf("") }
    var fingerprint by remember(prefilledFingerprint) { mutableStateOf(prefilledFingerprint) }

    val canSubmit = url.isNotBlank() && code.isNotBlank() && name.isNotBlank() &&
        fingerprint.isNotBlank() && !busy

    GhostScaffold { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).padding(24.dp).verticalScroll(rememberScrollState()),
            horizontalAlignment = Alignment.Start,
        ) {
            Spacer(Modifier.height(8.dp))
            SectionLabel("CONNECT TO YOUR BOX")
            Spacer(Modifier.height(8.dp))
            Text("This phone is a limb of your box. Enrol it once, the box mounts a persona for the " +
                 "pairing code you enter, and nothing of your life is stored here.",
                 color = GhostTextDim, style = MaterialTheme.typography.bodyMedium)

            Spacer(Modifier.height(24.dp))

            Field("BOX ADDRESS", url, { url = it }, "https://your-box.local:8443",
                keyboard = KeyboardType.Uri)
            Spacer(Modifier.height(6.dp))
            GhostButton("SCAN QR FROM THE BOX", onScanQr, modifier = Modifier.fillMaxWidth())

            Spacer(Modifier.height(20.dp))
            Field("PAIRING CODE", code, { code = it }, "one-time code shown on the box",
                keyboard = KeyboardType.Password)

            Spacer(Modifier.height(20.dp))
            Field("DEVICE NAME", name, { name = it }, "e.g. Vlad's S26")

            Spacer(Modifier.height(20.dp))
            Field("BOX FINGERPRINT", fingerprint, { fingerprint = it },
                "SHA-256 shown on the box (or scan the QR)", keyboard = KeyboardType.Ascii)
            if (fingerprint.isNotBlank()) {
                Spacer(Modifier.height(6.dp))
                Text("This is the box identity your phone will pin. It must match exactly, or the " +
                     "connection is refused.", color = TerminalDim,
                    style = MaterialTheme.typography.labelMedium)
            }

            if (error != null) {
                Spacer(Modifier.height(16.dp))
                Text(error, color = Warning, style = MaterialTheme.typography.bodyMedium)
            }

            Spacer(Modifier.height(28.dp))
            GhostButton(
                if (busy) "ENROLLING..." else "ENROL THIS DEVICE",
                { if (canSubmit) onEnroll(url.trim(), code.trim(), name.trim(), fingerprint.trim()) },
                modifier = Modifier.fillMaxWidth(),
                enabled = canSubmit,
            )

            Spacer(Modifier.height(20.dp))
            Text("The only cloud is you.", color = TerminalDim,
                style = MaterialTheme.typography.labelMedium,
                textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth())
        }
    }
}

@Composable
private fun Field(
    label: String,
    value: String,
    onChange: (String) -> Unit,
    hint: String,
    keyboard: KeyboardType = KeyboardType.Text,
) {
    Column(Modifier.fillMaxWidth()) {
        Text(label, color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        Spacer(Modifier.height(4.dp))
        OutlinedTextField(
            value = value,
            onValueChange = onChange,
            placeholder = { Text(hint, color = GhostTextDim) },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = keyboard),
            shape = RoundedCornerShape(12.dp),
            colors = OutlinedTextFieldDefaults.colors(
                focusedTextColor = GhostText,
                unfocusedTextColor = GhostText,
                focusedBorderColor = TerminalGreen,
                unfocusedBorderColor = GhostBorder,
                cursorColor = TerminalGreen,
            ),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}
