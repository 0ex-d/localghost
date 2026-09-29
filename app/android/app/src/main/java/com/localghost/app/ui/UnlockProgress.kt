package com.localghost.app.ui

import androidx.compose.animation.Crossfade
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.localghost.app.local.TransferRate
import com.localghost.app.net.StageState
import com.localghost.app.net.UnlockClock
import com.localghost.app.net.UnlockClockStore
import com.localghost.app.net.UnlockEstimate
import com.localghost.app.net.UnlockSnapshot
import com.localghost.app.net.UnlockStage
import com.localghost.app.ui.theme.GhostBorder
import com.localghost.app.ui.theme.GhostTextDim
import com.localghost.app.ui.theme.TerminalDim
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.Warning

/**
 * The unlock (and lock) screen's progress: a percentage and the time left, a bar that moves with
 * time, the box's steps each with how long it took, and a line underneath that says what the box
 * is doing now, turn about with something the app can do.
 *
 * The time comes from [UnlockClock]: this phone times every cold unlock and learns what each step
 * takes on this box; while the model loads, the box's own estimate is used. Everything shown is
 * built from the stage stream alone, which the box sends identically for every account, so a real
 * and a duress unlock look the same.
 */
@Composable
fun UnlockProgress(snapshot: UnlockSnapshot, modifier: Modifier = Modifier) {
    val ctx = LocalContext.current
    val kind = snapshot.stages.firstOrNull()?.stage // RESOLVE: an unlock; STOP_SERVICES: a lock
    val locking = kind == UnlockStage.STOP_SERVICES
    val clock = remember(kind) { UnlockClock(UnlockClockStore.load(ctx)) }
    val seed = remember(kind) { (System.nanoTime() % 1000).toInt() }
    // a quarter-second clock for the live times; the tidbit turns every 4.5 s
    val tick: Long by produceState(0L) {
        while (true) { kotlinx.coroutines.delay(250); value += 1 }
    }
    val est: UnlockEstimate = remember(snapshot, tick) { clock.observe(snapshot); clock.estimate() }
    LaunchedEffect(snapshot.done) {
        if (snapshot.done) clock.learn()?.let { UnlockClockStore.save(ctx, it) }
    }

    Column(modifier.fillMaxWidth()) {
        // 42%   about 20 s left
        Row(verticalAlignment = Alignment.Bottom) {
            Text("${(est.fraction * 100).toInt()}%", color = TerminalGreen,
                fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.headlineMedium)
            Spacer(Modifier.width(12.dp))
            Text(leftText(snapshot, est, locking), color = if (est.overdue) Warning else GhostTextDim,
                style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(bottom = 6.dp))
        }
        Spacer(Modifier.height(8.dp))
        ProgressBar(est.fraction, failed = snapshot.failed != null)
        Spacer(Modifier.height(16.dp))

        StepLine(snapshot, est)

        snapshot.failed?.let {
            Spacer(Modifier.height(10.dp))
            Text("! $it", color = Warning, style = MaterialTheme.typography.bodyMedium)
        }
        if (!snapshot.done && snapshot.failed == null) {
            Spacer(Modifier.height(18.dp))
            val turn = (tick / 18).toInt()
            val line = if (locking) "> " + UnlockTidbits.doing(est.current, snapshot.model)
                else UnlockTidbits.line(turn, est.current, snapshot.model, seed)
            Crossfade(targetState = line, animationSpec = tween(450), label = "tidbit") { text ->
                Text(text, color = if (text.startsWith(">")) TerminalDim else GhostTextDim,
                    style = MaterialTheme.typography.bodySmall, minLines = 2)
            }
        }
    }
}

@Composable
private fun ProgressBar(fraction: Float, failed: Boolean) {
    val shown by animateFloatAsState(fraction.coerceIn(0f, 1f), tween(600), label = "bar")
    Box(Modifier.fillMaxWidth().height(6.dp).background(GhostBorder)) {
        Box(Modifier.fillMaxWidth(shown).fillMaxHeight().background(if (failed) Warning else TerminalGreen))
    }
}

/** One line: "step 4 of 7 · starting database" and that step's time so far (the model's own
 *  percent while it loads). The list of every step was one too many things to read. */
@Composable
private fun StepLine(snap: UnlockSnapshot, est: UnlockEstimate) {
    // the last stage (ready / locked) is the arrival, not a step to wait through
    val steps = snap.stages.dropLast(1)
    val cur = est.current
    val idx = steps.indexOfFirst { it.stage == cur }
    val failedAt = snap.stages.firstOrNull { it.state == StageState.ERRORED }?.stage
    val text = when {
        failedAt != null -> "stopped at step ${steps.indexOfFirst { it.stage == failedAt } + 1} of ${steps.size} · ${failedAt.label}"
        snap.done || cur == null || idx < 0 -> "all ${steps.size} steps done"
        else -> "step ${idx + 1} of ${steps.size} · ${cur.label}"
    }
    // the running step breathes, so a long one still looks alive
    val t = rememberInfiniteTransition(label = "step")
    val pulse = if (!snap.done && failedAt == null) t.animateFloat(0.55f, 1f, infiniteRepeatable(tween(700), RepeatMode.Reverse), label = "pulse").value else 1f
    val took = cur?.let { est.took[it] }
    val pct = snap.model?.pct?.takeIf { cur == UnlockStage.MODEL && it in 1..99 }
    val right = if (snap.done || failedAt != null) "" else listOfNotNull(pct?.let { "$it%" }, took?.let { dur(it) }).joinToString("  ")
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(text, color = if (failedAt != null) Warning else TerminalGreen, fontFamily = FontFamily.Monospace,
            style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f).alpha(pulse))
        Text(right, color = TerminalDim, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.labelMedium)
    }
}

private fun leftText(s: UnlockSnapshot, e: UnlockEstimate, locking: Boolean): String = when {
    s.failed != null -> "stopped"
    s.done -> if (locking) "locked" else if (e.warm) "ready , it was already open" else "ready"
    e.overdue -> "taking longer than usual (${dur(e.took[e.current] ?: 0)} on this step)"
    e.secondsLeft < 0 -> "measuring…"
    !e.confident -> TransferRate.left(e.secondsLeft).replace("about", "roughly") + " (first time on this phone)"
    else -> TransferRate.left(e.secondsLeft)
}

/** 0.4 s, 12 s, 1:05 */
internal fun dur(ms: Long): String = when {
    ms < 10_000 -> String.format(java.util.Locale.US, "%.1f s", ms / 1000.0)
    ms < 60_000 -> "${ms / 1000} s"
    else -> String.format(java.util.Locale.US, "%d:%02d", ms / 60_000, (ms / 1000) % 60)
}
