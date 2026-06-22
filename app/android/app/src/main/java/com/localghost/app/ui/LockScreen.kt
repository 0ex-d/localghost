package com.localghost.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.foundation.Image
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.text.font.FontWeight
import com.localghost.app.ui.theme.GhostTextDim
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.Warning
import com.localghost.app.R

@Composable
fun LockScreen(error: String?, onUnlock: () -> Unit) {
    GhostScaffold { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).padding(32.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Image(
                painter = painterResource(R.drawable.ic_ghost),
                contentDescription = null,
                contentScale = ContentScale.Fit,
                modifier = Modifier.size(72.dp),
            )
            Spacer(Modifier.height(16.dp))
            // Wordmark fits on one line: size scales to the available width.
            BoxWithConstraints(Modifier.fillMaxWidth()) {
                // ~0.165 * width keeps the 10-char wordmark on one line comfortably.
                val fs = (maxWidth.value * 0.165f).coerceIn(28f, 56f)
                Text("LOCALGHOST", color = TerminalGreen,
                    fontSize = fs.sp, fontWeight = FontWeight.Bold, maxLines = 1, softWrap = false,
                    textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth())
            }
            Spacer(Modifier.height(8.dp))
            Text("> the only cloud is you", color = GhostTextDim,
                style = MaterialTheme.typography.bodyMedium)
            Spacer(Modifier.height(48.dp))
            GhostButton("UNLOCK", onUnlock, modifier = Modifier.fillMaxWidth())
            error?.let {
                Spacer(Modifier.height(16.dp))
                Text("! $it", color = Warning, textAlign = TextAlign.Center,
                    style = MaterialTheme.typography.bodyMedium)
            }
        }
    }
}
