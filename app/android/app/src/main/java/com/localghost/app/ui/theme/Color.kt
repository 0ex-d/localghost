package com.localghost.app.ui.theme

import androidx.compose.ui.graphics.Color

// LocalGhost palette — deliberately limited. Black, green, grey, red. No gradients.
// https://www.localghost.ai/brand-guidelines  (Section 03)
val Void = Color(0xFF111111)         // primary background, near-black with residual CRT glow
val VoidLighter = Color(0xFF1A1A1A)  // cards, code blocks, secondary surfaces
val TerminalGreen = Color(0xFF33FF00) // primary accent — links, emphasis, "our" values. Sacred.
val TerminalDim = Color(0xFF1A8000)  // muted green for borders, shadows, glow
val GhostText = Color(0xFFE0E0E0)    // primary text, warm white
val GhostTextDim = Color(0xFFA0A0A0) // secondary text, metadata, placeholders
val GhostBorder = Color(0xFF444444)  // dividers, structural lines
val Warning = Color(0xFFFF8A8A)      // danger, errors, "their" way. Use sparingly.
