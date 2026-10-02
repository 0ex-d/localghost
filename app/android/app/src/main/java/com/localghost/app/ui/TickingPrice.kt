package com.localghost.app.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.Warning

/**
 * A PRICE THAT MOVES when the box's number does: each changed digit rolls (up when the price
 * rose, down when it fell) and the whole figure flashes green or amber and fades back, so a
 * five-second update is seen, not just read. The first value shows still.
 */
@Composable
fun TickingPrice(text: String, value: Double, color: Color, style: TextStyle, modifier: Modifier = Modifier, fontSize: TextUnit = TextUnit.Unspecified) {
    // the direction is worked out in the composition that brings the new value, so the digits
    // that change in it already know which way to roll
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
    val flash = remember { Animatable(0f) }
    LaunchedEffect(value) {
        if (dir != 0) {
            flash.snapTo(1f)
            flash.animateTo(0f, tween(durationMillis = 1200))
        }
    }
    val up = wasUp[0]
    val tint: Color = if (up) TerminalGreen else Warning
    val shown: Color = lerp(color, tint, flash.value)
    Row(modifier.clipToBounds().background(tint.copy(alpha = 0.16f * flash.value), RoundedCornerShape(3.dp)).padding(horizontal = 2.dp)) {
        // keyed from the right, so a price that gains a digit (99,999 to 100,000) rolls in place
        text.forEachIndexed { i, ch ->
            key(text.length - i) {
                AnimatedContent(
                    targetState = ch,
                    transitionSpec = {
                        val dir = if (up) 1 else -1
                        ((slideInVertically(tween(260)) { h -> dir * h } + fadeIn(tween(260))) togetherWith
                            (slideOutVertically(tween(260)) { h -> -dir * h } + fadeOut(tween(200))))
                            .using(SizeTransform(clip = true))
                    },
                    label = "digit",
                ) { c ->
                    Text(c.toString(), color = shown, style = style, fontSize = fontSize)
                }
            }
        }
    }
}
