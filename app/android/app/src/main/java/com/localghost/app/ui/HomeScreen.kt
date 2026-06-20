package com.localghost.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.localghost.app.ui.theme.GhostText
import com.localghost.app.ui.theme.GhostTextDim
import com.localghost.app.ui.theme.TerminalGreen

@Composable
fun HomeScreen() {
    GhostScaffold { pad ->
        Column(Modifier.fillMaxSize().padding(pad).padding(32.dp)) {
            SectionLabel("SESSION_ACTIVE")
            Spacer(Modifier.height(12.dp))
            Text("CONNECTED",
                color = TerminalGreen,
                style = glow(MaterialTheme.typography.titleLarge))
            Spacer(Modifier.height(16.dp))
            TerminalPrompt("mount ok — memory layer attached")
            Spacer(Modifier.height(24.dp))
            // The persona's own marker + data render here once the box APIs land.
            // You recognise which persona you're in by YOUR content/marker — never
            // a status badge the box doesn't know to hide from a coercer.
            Text("Capture, sync and reflection land here next.",
                color = GhostTextDim, style = MaterialTheme.typography.bodyMedium)
        }
    }
}
