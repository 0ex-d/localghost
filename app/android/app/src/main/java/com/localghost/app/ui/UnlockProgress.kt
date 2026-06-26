package com.localghost.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.localghost.app.net.StageState
import com.localghost.app.net.UnlockSnapshot
import com.localghost.app.ui.theme.GhostTextDim
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.Warning

/**
 * Renders the streamed unlock stages as a terminal-style list. Every stage in the box's fixed order
 * is shown; each ticks from pending -> running -> complete (or skipped, for a hot account) as poll
 * snapshots arrive. Because the box streams the identical stage set for every account, this view is
 * identical for a real or a duress unlock , the cold loading sequence a user can point at and say
 * "it's loading" looks the same whichever account opened.
 */
@Composable
fun UnlockProgress(snapshot: UnlockSnapshot, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        snapshot.stages.forEach { row ->
            StageLine(row.stage.label, row.state)
        }
        snapshot.failed?.let {
            Spacer(Modifier.height(8.dp))
            Text("! $it", color = Warning, style = MaterialTheme.typography.bodyMedium)
        }
    }
}

@Composable
private fun StageLine(label: String, state: StageState) {
    val (mark, color) = when (state) {
        StageState.PENDING  -> "  " to GhostTextDim
        StageState.RUNNING  -> "> " to TerminalGreen
        StageState.SKIPPED  -> "- " to GhostTextDim   // already warm: nothing to do
        StageState.COMPLETE -> "* " to TerminalGreen
        StageState.ERRORED  -> "! " to Warning
    }
    val suffix = when (state) {
        StageState.RUNNING  -> " ..."
        StageState.SKIPPED  -> " (ready)"
        else -> ""
    }
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            "$mark$label$suffix",
            color = color,
            fontFamily = FontFamily.Monospace,
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}
