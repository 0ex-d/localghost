package com.localghost.app.ui.theme

import android.app.Activity
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalView
import androidx.core.view.WindowCompat

// Fixed brand scheme. No dynamic colour — the wallpaper does not get a vote.
private val GhostColors = darkColorScheme(
    primary = TerminalGreen,
    onPrimary = Void,
    secondary = TerminalDim,
    onSecondary = Void,
    background = Void,
    onBackground = GhostText,
    surface = VoidLighter,
    onSurface = GhostText,
    surfaceVariant = VoidLighter,
    onSurfaceVariant = GhostTextDim,
    outline = GhostBorder,
    error = Warning,
    onError = Void,
)

@Composable
fun LocalGhostTheme(content: @Composable () -> Unit) {
    val view = LocalView.current
    if (!view.isInEditMode) {
        SideEffect {
            val window = (view.context as Activity).window
            window.statusBarColor = Void.toArgb()
            window.navigationBarColor = Void.toArgb()
            WindowCompat.getInsetsController(window, view).apply {
                isAppearanceLightStatusBars = false   // light icons on the void
                isAppearanceLightNavigationBars = false
            }
        }
    }
    MaterialTheme(
        colorScheme = GhostColors,
        typography = GhostTypography,
        content = content,
    )
}
