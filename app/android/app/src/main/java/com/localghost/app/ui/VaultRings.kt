package com.localghost.app.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutLinearInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.withFrameMillis
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.painterResource
import com.localghost.app.R
import com.localghost.app.net.UnlockSnapshot
import com.localghost.app.ui.VaultRingsModel.Phase
import com.localghost.app.ui.theme.TerminalDim
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.Warning
import kotlin.math.PI
import kotlin.math.exp
import kotlin.math.min
import kotlin.math.sin

/**
 * THE VAULT RINGS: the unlock and the lock, drawn. One segmented ring per step of the unlock
 * around the ghost ([VaultRingsModel]). A ring whose step is running spins and fills, with a
 * bright head sweeping round it; when the box says the step is done it lights whole and swings
 * round until its keyway sits at the top, and the phone ticks. When every ring is locked in, the
 * keyways line up into one slot above the ghost. At READY ([release] on an unlock) the rings open
 * like an iris, each flying outward as it splits into dashes, and the app is behind them.
 *
 * A lock is the same machine run backwards. The rings fly in from outside and close round the
 * ghost like the iris shutting, all lit and lined up; then, from the inside out, each teardown step
 * puts out the ring of the step it undoes, and the ring comes loose and spins down dark. At LOCKED ([release]
 * on a lock) the whole thing switches off like an old monitor: squeezed to a bright line, the line
 * to a dot, the dot out.
 *
 * Everything moves with the stage stream the box sends, which is the same for every account and,
 * since the box replays a cold unlock on a warm one, the same length either way. Drawn on one
 * Canvas in the app's phosphor green; the frame clock is read only while drawing, so the screen
 * around it does not recompose sixty times a second.
 */
@Composable
fun VaultRings(snapshot: UnlockSnapshot, locking: Boolean, release: Boolean, modifier: Modifier = Modifier) {
    val phases = VaultRingsModel.phases(snapshot, locking)
    val n = phases.size
    val haptic = LocalHapticFeedback.current

    // the frame clock, ms since the rings appeared
    val clock: androidx.compose.runtime.MutableLongState = remember { mutableLongStateOf(0L) }
    LaunchedEffect(Unit) {
        val t0 = withFrameMillis { it }
        while (true) withFrameMillis { clock.longValue = it - t0 }
    }
    // when each ring last changed phase, and whether it was ever locked in (read while drawing)
    val marks: Array<Mark> = remember { Array(n) { Mark() } }
    SideEffect {
        val now = clock.longValue
        phases.forEachIndexed { i, p ->
            val m = marks[i]
            if (m.phase == p) return@forEachIndexed
            if (m.phase == null) {
                // first sight: a lock starts with every ring already locked in, not swinging into place
                m.phase = p; m.at = if (p == Phase.LIT) -STARTED_LIT else now
                if (p == Phase.LIT) m.restAngle = REST
                return@forEachIndexed
            }
            if (p == Phase.LIT) {
                m.lockFrom = spinAngle(i, now, m)
                m.restAngle = VaultRingsModel.restAngle(m.lockFrom, clockwise(i))
            }
            if (m.phase == Phase.LIT) m.leftLitAt = now
            m.phase = p; m.at = now
        }
    }
    val settled = VaultRingsModel.settled(phases, locking)
    LaunchedEffect(settled) { if (settled > 0) haptic.performHapticFeedback(HapticFeedbackType.TextHandleMove) }
    LaunchedEffect(release) { if (release) haptic.performHapticFeedback(HapticFeedbackType.LongPress) }

    // A LOCK OPENS THE WAY AN UNLOCK ENDS, BACKWARDS: the rings fly in from outside and close like
    // an iris round the ghost, all lit and lined up, with a clunk; then the teardown puts them out.
    val arrive = remember { Animatable(if (locking) 1f else 0f) }
    LaunchedEffect(locking) {
        if (locking) {
            arrive.animateTo(0f, tween(ARRIVE_MS, easing = LinearEasing))
            haptic.performHapticFeedback(HapticFeedbackType.LongPress)
        }
    }
    // the iris waits a beat first, so the last ring is seen to lock in and the slot to line up
    val rel: Float by animateFloatAsState(
        targetValue = if (release) 1f else 0f,
        animationSpec = if (locking) tween(CLOSE_MS, easing = FastOutLinearInEasing)
            else tween(OPEN_MS, delayMillis = OPEN_HOLD_MS, easing = LinearEasing),
        label = "vault-release",
    )

    Box(
        modifier.graphicsLayer {
            if (locking && rel > 0f) {
                // the old monitor switching off: squeeze to a line, the line to a dot, the dot out
                val a = (rel / 0.55f).coerceIn(0f, 1f)
                val b = ((rel - 0.55f) / 0.30f).coerceIn(0f, 1f)
                scaleY = 1f - 0.985f * a * a
                scaleX = (1f + 0.12f * a) * (1f - 0.97f * b)
                alpha = 1f - ((rel - 0.85f) / 0.15f).coerceIn(0f, 1f)
                transformOrigin = TransformOrigin.Center
            }
        },
        contentAlignment = Alignment.Center,
    ) {
        Canvas(Modifier.fillMaxSize()) {
            drawVault(clock.longValue, phases, marks, rel, arrive.value, locking)
        }
        // the ghost at the centre: it brightens and grows a little as the iris opens
        Image(
            painter = painterResource(R.drawable.ic_ghost), contentDescription = null,
            modifier = Modifier.fillMaxWidth(0.26f).graphicsLayer {
                val open = easeInOut(if (!locking) rel else arrive.value)
                scaleX = 1f + 0.35f * open; scaleY = scaleX
                alpha = 1f - 0.25f * open
            },
        )
    }
}

/** How long the iris takes to open (after a beat for the last ring to lock in), and the monitor to
 *  switch off. The caller waits these out. */
private const val OPEN_HOLD_MS = 450
private const val OPEN_MS = 800
private const val CLOSE_MS = 650
const val VAULT_OPEN_MS = OPEN_HOLD_MS + OPEN_MS
const val VAULT_CLOSE_MS = CLOSE_MS

/** How long a lock's rings take to fly in and close round the ghost, before the teardown shows.
 *  Declared before ARRIVE_MS: a constant cannot be read before its own line. */
const val VAULT_ARRIVE_MS = 750
private const val ARRIVE_MS = VAULT_ARRIVE_MS

private const val REST = -90f          // the keyway's resting angle: straight up
private const val STARTED_LIT = 60_000L // "locked in long ago"
private const val LOCK_IN_MS = 420f
private const val GAP_DEG = 3.2f

private class Mark {
    var phase: Phase? = null
    var at = 0L
    var lockFrom = 0f
    var restAngle = REST
    var leftLitAt = -1L
}

private fun clockwise(ring: Int) = ring % 2 == 0

/** Degrees per millisecond: outer rings slower, neighbours in opposite directions. */
private fun speed(ring: Int) = (0.010f + 0.004f * ring) * if (clockwise(ring)) 1f else -1f

/** Where a ring that is not locked in points now. */
private fun spinAngle(ring: Int, t: Long, m: Mark): Float {
    if (m.leftLitAt >= 0) {
        // came loose from its locked position: spins up from rest over 0.6 s
        val dt = (t - m.leftLitAt).toFloat()
        val ramp = 600f
        val travel = if (dt < ramp) dt * dt / (2 * ramp) else dt - ramp / 2
        return m.restAngle + speed(ring) * 1.6f * travel
    }
    return ring * 47f + speed(ring) * t
}

/** easeInOutCubic: the iris starts slowly, opens fast, settles. */
private fun easeInOut(x: Float): Float = if (x < 0.5f) 4f * x * x * x else 1f - (-2f * x + 2f).let { it * it * it } / 2f

/** easeOutBack: overshoots a touch and settles, the "clunk" of a ring locking in. */
private fun easeOutBack(x: Float): Float {
    val c1 = 1.70158f
    val c3 = c1 + 1f
    val y = x - 1f
    return 1f + c3 * y * y * y + c1 * y * y
}

private fun DrawScope.drawVault(t: Long, phases: List<Phase>, marks: Array<Mark>, rel: Float, arrive: Float, locking: Boolean) {
    val outer = min(size.width, size.height) / 2f * 0.95f
    val c = Offset(size.width / 2f, size.height / 2f)
    val n = phases.size
    val step = outer * (1f - 0.40f) / (n - 1)
    val stroke = step * 0.42f
    // how far the iris is open (an unlock opening it, a lock closing it) and how much is left to see
    val iris = if (!locking) rel else arrive
    val open = easeInOut(iris)
    val openFade = 1f - iris

    // the scope: four ticks outside the outermost ring
    for (k in 0 until 4) {
        val a = (k * 90f) * (PI / 180f).toFloat()
        val r1 = outer + stroke * 0.9f
        val r2 = outer + stroke * 1.8f
        drawLine(TerminalDim.copy(alpha = 0.55f * openFade),
            Offset(c.x + r1 * kotlin.math.cos(a), c.y + r1 * sin(a)),
            Offset(c.x + r2 * kotlin.math.cos(a), c.y + r2 * sin(a)), strokeWidth = stroke * 0.25f)
    }

    var aligned = 0
    for (i in 0 until n) {
        val m = marks[i]
        val p = m.phase ?: phases[i]
        val dt = (t - m.at).toFloat()
        val angle = when (p) {
            Phase.LIT -> if (m.at < 0) {
                aligned++
                m.restAngle
            } else {
                val d = kotlin.math.abs(m.restAngle - m.lockFrom)
                val x = (dt / (LOCK_IN_MS + d * 0.4f)).coerceIn(0f, 1f)
                if (x >= 1f) aligned++
                m.lockFrom + (m.restAngle - m.lockFrom) * easeOutBack(x)
            }
            else -> spinAngle(i, t, m)
        }
        val fill = when (p) {
            Phase.DARK -> 0f
            Phase.FILLING -> 0.9f * (1f - exp(-dt / 900f))
            Phase.LIT, Phase.ERROR -> 1f
            Phase.DRAINING -> (1f - dt / 320f).coerceIn(0f, 1f)
        }
        // just locked in: a flash that fades over 0.35 s
        val flash = if (p == Phase.LIT && m.at >= 0) (1f - dt / 350f).coerceIn(0f, 1f) else 0f
        val flicker = if (p == Phase.ERROR) 0.55f + 0.45f * sin(t / 45f) else 1f
        val lit = if (p == Phase.ERROR) Warning else TerminalGreen

        // the iris: outer rings fly furthest; segments shrink to dashes; everything fades
        val radius = outer * VaultRingsModel.radiusShare(i) * (1f + open * (0.30f + 0.14f * (n - 1 - i)))
        val fade = openFade
        val segs = VaultRingsModel.segments(i)
        val per = 360f / segs
        val sweep = (per - GAP_DEG) * (1f - 0.7f * open)
        val litSegs = fill * (segs - 1) // the keyway (segment 0) never lights
        for (s in 1 until segs) {
            val start = angle + s * per + (per - sweep) / 2f
            val on = s - 1 < litSegs
            val partial = (litSegs - (s - 1)).coerceIn(0f, 1f)
            if (on) {
                // the glow under, then the segment
                drawSeg(c, radius, start, sweep, stroke * 2.3f, lit.copy(alpha = (0.14f + 0.25f * flash) * fade * flicker * partial))
                drawSeg(c, radius, start, sweep, stroke, lit.copy(alpha = (0.55f + 0.45f * partial) * fade * flicker))
            } else {
                drawSeg(c, radius, start, sweep, stroke, TerminalDim.copy(alpha = 0.22f * fade))
            }
        }
        // a running step: a bright head sweeps round the ring once a second
        if (p == Phase.FILLING) {
            val head = angle + (t % 1000L) / 1000f * 360f * if (clockwise(i)) 1f else -1f
            drawSeg(c, radius, head, 14f, stroke * 1.15f, TerminalGreen.copy(alpha = 0.9f))
        }
    }

    // every ring locked in: the keyways make one slot above the ghost
    if (aligned == n) {
        val top = c.y - outer - stroke / 2f
        val bottom = c.y - outer * VaultRingsModel.radiusShare(n - 1) + stroke / 2f
        drawLine(TerminalGreen.copy(alpha = 0.85f * openFade), Offset(c.x, top), Offset(c.x, bottom), strokeWidth = stroke * 0.35f, cap = StrokeCap.Round)
    }

    // the iris opening: a burst of light from the centre
    if (open > 0f) {
        // a soft glow that swells from the centre and fades at its edge (no hard disc)
        val r = outer * (0.25f + 0.85f * open)
        drawCircle(
            Brush.radialGradient(listOf(TerminalGreen.copy(alpha = 0.34f * openFade), Color.Transparent), center = c, radius = r),
            radius = r, center = c,
        )
    }
    // switching off: the picture goes bright as it squeezes into a line
    if (locking && rel > 0f) {
        // a band brightest along the middle: squeezed, it is the bright line an old tube leaves
        val a = (rel / 0.55f).coerceIn(0f, 1f)
        drawRect(Brush.verticalGradient(
            0f to Color.Transparent, 0.5f to TerminalGreen.copy(alpha = 0.75f * a), 1f to Color.Transparent), size = size)
    }
    // phosphor scanlines over everything, faint
    val gap = 3f * density
    var y = 0f
    while (y < size.height) {
        drawLine(Color.Black.copy(alpha = 0.18f), Offset(0f, y), Offset(size.width, y), strokeWidth = density * 0.8f)
        y += gap
    }
}

private fun DrawScope.drawSeg(c: Offset, r: Float, start: Float, sweep: Float, width: Float, color: Color) {
    if (color.alpha <= 0.003f) return
    drawArc(color, startAngle = start, sweepAngle = sweep, useCenter = false,
        topLeft = Offset(c.x - r, c.y - r), size = Size(r * 2, r * 2), style = Stroke(width = width, cap = StrokeCap.Butt))
}
