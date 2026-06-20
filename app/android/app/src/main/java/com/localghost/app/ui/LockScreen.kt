package com.localghost.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.localghost.app.ui.theme.GhostTextDim
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.Warning

@Composable
fun LockScreen(error: String?, onUnlock: () -> Unit) {
    GhostScaffold { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).padding(32.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            TerminalPrompt()
            Spacer(Modifier.height(40.dp))
            Text(
                "LOCALGHOST",
                color = TerminalGreen,
                style = glow(MaterialTheme.typography.titleLarge.copy(letterSpacing = 4.sp)),
            )
            Spacer(Modifier.height(12.dp))
            Text(
                "THE ONLY CLOUD IS YOU",
                color = GhostTextDim,
                style = MaterialTheme.typography.labelMedium.copy(letterSpacing = 2.sp),
            )
            Spacer(Modifier.height(48.dp))
            GhostButton("UNLOCK", onUnlock, modifier = Modifier.fillMaxWidth())
            error?.let {
                Spacer(Modifier.height(24.dp))
                Text(
                    "! $it",
                    color = Warning,
                    style = MaterialTheme.typography.bodyMedium,
                    textAlign = TextAlign.Center,
                )
            }
        }
    }
}
