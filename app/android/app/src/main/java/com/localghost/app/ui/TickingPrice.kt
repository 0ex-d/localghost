package com.localghost.app.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.isSpecified
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.Warning

/** A soft overshoot: the digit lands a touch past its place and settles. */
private val EaseOutBack = CubicBezierEasing(0.34f, 1.56f, 0.64f, 1f)

/**
 * A PRICE THAT MOVES when the box's number does, like an odometer: only the digits that changed
 * roll, up when the price rose and down when it fell, the rightmost first and each one to its
 * left a beat later, landing with a small overshoot; each rolled digit glows green or amber and
 * fades back to the text colour, and an arrow beside the price says which way and fades out.
 * Commas and the point stay still. The first value shows still.
 */
@Composable
fun TickingPrice(text: String, value: Double, color: Color, style: TextStyle, modifier: Modifier = Modifier,
                 fontSize: TextUnit = TextUnit.Unspecified, arrow: Boolean = false) {
    // the direction, worked out in the composition that brings the new value, so the digits that
    // change in it already know which way to roll
    val prev = remember { doubleArrayOf(value) }
    val wasUp = remember { booleanArrayOf(true) }
    val dir: Int = remember(value) {
        val d = when {
            prev[0] <= 0 || value <= 0 || value == prev[0] -> 0
            value > prev[0] -> 1
            else -> -1
        }
        prev[0] = value
        if (d != 0) wasUp[0] = d > 0
        d
    }
    val up = wasUp[0]
    val tint: Color = if (up) TerminalGreen else Warning
    val arrowAlpha = remember { Animatable(0f) }
    LaunchedEffect(value) {
        if (dir != 0 && arrow) {
            arrowAlpha.snapTo(1f)
            arrowAlpha.animateTo(0f, tween(durationMillis = 1600, delayMillis = 400))
        }
    }
    Row(modifier.clipToBounds(), verticalAlignment = Alignment.CenterVertically) {
        // keyed from the right, so a price that gains a digit (99,999 to 100,000) rolls in place
        text.forEachIndexed { i, ch ->
            val fromRight = text.length - 1 - i
            key(fromRight) {
                RollingChar(ch, up, color, tint, style, fontSize, delayMs = minOf(fromRight, 6) * 35)
            }
        }
        if (arrow) {
            // half the price's size (an unspecified size cannot be multiplied: the style's then)
            val af: TextUnit = when {
                fontSize.isSpecified -> fontSize * 0.5f
                style.fontSize.isSpecified -> style.fontSize * 0.5f
                else -> TextUnit.Unspecified
            }
            Text(if (up) " ▲" else " ▼", color = tint, style = style, fontSize = af,
                modifier = Modifier.alpha(arrowAlpha.value))
        }
    }
}

/** One character: a digit rolls to its next value and glows while it settles; anything else is still. */
@Composable
private fun RollingChar(ch: Char, up: Boolean, color: Color, tint: Color, style: TextStyle, fontSize: TextUnit, delayMs: Int) {
    val glow = remember { Animatable(0f) }
    val first = remember { booleanArrayOf(true) }
    LaunchedEffect(ch) {
        if (first[0]) {
            first[0] = false
            return@LaunchedEffect
        }
        if (ch.isDigit()) {
            glow.snapTo(1f)
            glow.animateTo(0f, tween(durationMillis = 1400, delayMillis = delayMs + 250))
        }
    }
    AnimatedContent(
        targetState = ch,
        transitionSpec = {
            if (!targetState.isDigit() || !initialState.isDigit()) {
                fadeIn(tween(150)) togetherWith fadeOut(tween(100))
            } else {
                val d = if (up) 1 else -1
                ((slideInVertically(tween(420, delayMs, EaseOutBack)) { h -> d * h } + fadeIn(tween(220, delayMs))) togetherWith
                    (slideOutVertically(tween(320, delayMs, FastOutSlowInEasing)) { h -> -d * h } + fadeOut(tween(180, delayMs))))
                    .using(SizeTransform(clip = true))
            }
        },
        label = "digit",
    ) { c ->
        Text(c.toString(), color = lerp(color, tint, glow.value), style = style, fontSize = fontSize)
    }
}
