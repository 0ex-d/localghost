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
import androidx.compose.foundation.clickable
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.text.font.FontWeight
import com.localghost.app.ui.theme.GhostTextDim
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.Warning
import com.localghost.app.net.UnlockSnapshot
import androidx.compose.runtime.getValue
import com.localghost.app.R

/**
 * The lock screen. When unlocking is true it shows the streamed stage list (UnlockProgress) instead
 * of the UNLOCK button: a hot account fills in instantly, a cold one ticks through stages once a
 * second. The progress is identical for any account, so the cold loading view is the same whether a
 * real, decoy, or duress PIN was entered.
 */
@Composable
fun LockScreen(
    error: String?,
    unlocking: Boolean,
    progress: UnlockSnapshot?,
    onLocalOnly: () -> Unit = {},
    onReenroll: () -> Unit = {},
    closing: Boolean = false,
    onUnlock: () -> Unit,
) {
    GhostScaffold { pad ->
        // LOCKING: the vault rings go out from the inside, one teardown step at a time, then the
        // picture switches off like an old monitor ([closing]); the gate comes back after it.
        if (unlocking && progress != null) {
            Column(
                Modifier.fillMaxSize().padding(pad).padding(horizontal = 32.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                VaultRings(progress, locking = true, release = closing,
                    modifier = Modifier.fillMaxWidth(0.78f).widthIn(max = 300.dp).aspectRatio(1f))
                Spacer(Modifier.height(20.dp))
                Text("LOCKING", color = GhostTextDim, style = MaterialTheme.typography.titleMedium)
                Spacer(Modifier.height(20.dp))
                UnlockProgress(progress, modifier = Modifier.widthIn(max = 360.dp))
            }
            return@GhostScaffold
        }
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
            BoxWithConstraints(Modifier.fillMaxWidth()) {
                val fs = (maxWidth.value * 0.165f).coerceIn(28f, 56f)
                Text("LOCALGHOST", color = TerminalGreen,
                    fontSize = fs.sp, fontWeight = FontWeight.Bold, maxLines = 1, softWrap = false,
                    textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth())
            }
            Spacer(Modifier.height(8.dp))
            Text("> the only cloud is you", color = GhostTextDim,
                style = MaterialTheme.typography.bodyMedium)
            Spacer(Modifier.height(48.dp))

            run {
                GhostButton("UNLOCK", onUnlock, modifier = Modifier.fillMaxWidth())
                error?.let {
                    Spacer(Modifier.height(16.dp))
                    Text("! $it", color = Warning, textAlign = TextAlign.Center,
                        style = MaterialTheme.typography.bodyMedium)
                }
                Spacer(Modifier.height(24.dp))
                // Bracketed + underlined: these are BUTTONS and looked like footnotes , the app's
                // action grammar is [ VERB ], so secondary actions wear it too.
                Text("[ CAN'T REACH YOUR BOX? USE ON-PHONE MODELS ONLY ]", color = GhostTextDim,
                    textAlign = TextAlign.Center, style = MaterialTheme.typography.labelMedium,
                    textDecoration = TextDecoration.Underline,
                    modifier = Modifier.fillMaxWidth().clickable { onLocalOnly() })
                Spacer(Modifier.height(12.dp))
                Text("[ RE-SCAN THE BOX QR , RE-ENROL THIS PHONE ]", color = GhostTextDim,
                    textAlign = TextAlign.Center, style = MaterialTheme.typography.labelMedium,
                    textDecoration = TextDecoration.Underline,
                    modifier = Modifier.fillMaxWidth().clickable { onReenroll() })
            }
        }
    }
}
