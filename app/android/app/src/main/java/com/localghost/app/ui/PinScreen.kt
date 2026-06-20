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

@Composable
fun PinScreen(
    busy: Boolean,
    error: String?,
    onSubmit: (String) -> Unit,
) {
    var pin by remember { mutableStateOf("") }

    Scaffold { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).padding(24.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Text("Enter code")
            Spacer(Modifier.height(16.dp))

            // Masked, single field. We deliberately do NOT show a digit count —
            // length must not leak to a shoulder-surfer, and prefixes like 1223
            // vs 122338 only work because nothing auto-submits.
            OutlinedTextField(
                value = pin,
                onValueChange = { next -> if (next.all(Char::isDigit)) pin = next },
                singleLine = true,
                enabled = !busy,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                modifier = Modifier.fillMaxWidth()
            )

            Spacer(Modifier.height(24.dp))

            Button(
                onClick = { onSubmit(pin); pin = "" },
                enabled = pin.isNotEmpty() && !busy,
                modifier = Modifier.fillMaxWidth()
            ) { Text("OK") }

            if (busy) {
                Spacer(Modifier.height(24.dp))
                // The uniform wait. Every code — real, decoy, wipe, garbage — shows
                // this identical screen for the box's fixed-deadline window, so mount
                // time leaks nothing. The text is deliberately generic.
                CircularProgressIndicator()
                Spacer(Modifier.height(8.dp))
                Text("Loading…")
            }

            error?.let {
                Spacer(Modifier.height(16.dp))
                Text(it, textAlign = TextAlign.Center)
            }
        }
    }
}
