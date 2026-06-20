package com.localghost.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.localghost.app.ui.theme.*

@Composable
fun PinScreen(
    busy: Boolean,
    error: String?,
    onSubmit: (String) -> Unit,
) {
    var pin by remember { mutableStateOf("") }

    GhostScaffold { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).padding(32.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            SectionLabel("AUTH_REQUIRED", Modifier.align(Alignment.Start))
            Spacer(Modifier.height(8.dp))
            Text("ENTER CODE", color = GhostText, style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(24.dp))

            // Masked, variable length. No digit count rendered — length must not leak,
            // and prefixes (1223 vs 122338) only work because nothing auto-submits.
            OutlinedTextField(
                value = pin,
                onValueChange = { next -> if (next.all(Char::isDigit)) pin = next },
                singleLine = true,
                enabled = !busy,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedTextColor = TerminalGreen,
                    unfocusedTextColor = TerminalGreen,
                    cursorColor = TerminalGreen,
                    focusedBorderColor = TerminalGreen,
                    unfocusedBorderColor = GhostBorder,
                    focusedContainerColor = VoidLighter,
                    unfocusedContainerColor = VoidLighter,
                ),
                modifier = Modifier.fillMaxWidth(),
            )

            Spacer(Modifier.height(24.dp))
            GhostButton("OK", { onSubmit(pin); pin = "" },
                enabled = pin.isNotEmpty() && !busy, modifier = Modifier.fillMaxWidth())

            if (busy) {
                Spacer(Modifier.height(32.dp))
                // Uniform wait — identical for real / decoy / wipe / garbage.
                CircularProgressIndicator(color = TerminalGreen, strokeWidth = 2.dp)
                Spacer(Modifier.height(12.dp))
                Text("> LOADING", color = TerminalGreen, style = MaterialTheme.typography.bodyMedium)
            }

            error?.let {
                Spacer(Modifier.height(24.dp))
                Text("! $it", color = Warning,
                    style = MaterialTheme.typography.bodyMedium, textAlign = TextAlign.Center)
            }
        }
    }
}
