package com.localghost.app.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.localghost.app.ui.theme.GhostBorder
import com.localghost.app.ui.theme.GhostTextDim
import com.localghost.app.ui.theme.TerminalDim
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.Void

/** Subliminal CRT scanlines — 2px transparent / 2px rgba(0,0,0,0.1) fixed overlay. */
fun Modifier.scanlines(): Modifier = drawWithContent {
    drawContent()
    val gap = 4f
    var y = 0f
    while (y < size.height) {
        drawRect(
            color = Color.Black.copy(alpha = 0.10f),
            topLeft = Offset(0f, y),
            size = androidx.compose.ui.geometry.Size(size.width, 2f),
        )
        y += gap
    }
}

/** Phosphor glow for green text elements. text-shadow: 0 0 ~6px terminal-dim. */
fun glow(base: TextStyle): TextStyle =
    base.copy(shadow = Shadow(color = TerminalDim, offset = Offset.Zero, blurRadius = 14f))

/** Every screen sits on the void with scanlines over it. */
@Composable
fun GhostScaffold(content: @Composable (PaddingValues) -> Unit) {
    Surface(color = Void, modifier = Modifier.fillMaxSize()) {
        Box(Modifier.fillMaxSize().scanlines()) {
            Scaffold(containerColor = Color.Transparent, content = content)
        }
    }
}

/** > SECTION_XX label — terminal green, wide tracking. */
@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier) {
    Text(
        text = "> $text",
        color = TerminalGreen,
        style = MaterialTheme.typography.labelMedium.copy(letterSpacing = 3.sp),
        modifier = modifier,
    )
}

/** ghost@localghost prompt line. */
@Composable
fun TerminalPrompt(text: String = "ghost@localghost: ~", modifier: Modifier = Modifier) {
    Text(
        text = "> $text",
        color = GhostTextDim,
        style = MaterialTheme.typography.bodyMedium,
        modifier = modifier,
    )
}

/** Bracket button: 1px border, no fill until pressed, square corners. Pass label WITHOUT brackets. */
@Composable
fun GhostButton(
    label: String,
    onClick: () -> Unit,
    enabled: Boolean = true,
    modifier: Modifier = Modifier,
) {
    OutlinedButton(
        onClick = onClick,
        enabled = enabled,
        shape = RectangleShape,
        border = BorderStroke(1.dp, if (enabled) TerminalGreen else GhostBorder),
        colors = ButtonDefaults.outlinedButtonColors(
            contentColor = TerminalGreen,
            disabledContentColor = GhostBorder,
        ),
        modifier = modifier,
    ) {
        Text("[ $label ]", style = MaterialTheme.typography.labelLarge)
    }
}
