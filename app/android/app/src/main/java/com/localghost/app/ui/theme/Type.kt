package com.localghost.app.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

// PRIMARY TYPEFACE: JetBrains Mono. "No serif fonts. Ever." (Section 04)
//
// This file ships with FontFamily.Monospace so the APK builds with zero extra
// assets. To match the brand EXACTLY — and keep it offline, no Google Fonts
// runtime call (the only cloud is you) — drop the four OFL .ttf files into
//   app/src/main/res/font/
// named jetbrains_mono_light.ttf / _regular.ttf / _medium.ttf / _bold.ttf,
// then replace GhostFont below with the commented FontFamily.
val GhostFont: FontFamily = FontFamily.Monospace

/*  BRAND-EXACT VERSION — uncomment after adding the .ttf files, delete the line above:
val GhostFont: FontFamily = FontFamily(
    Font(R.font.jetbrains_mono_light,   FontWeight.Light),
    Font(R.font.jetbrains_mono_regular, FontWeight.Normal),
    Font(R.font.jetbrains_mono_medium,  FontWeight.Medium),
    Font(R.font.jetbrains_mono_bold,    FontWeight.Bold),
)
*/

val GhostTypography = Typography(
    displayLarge  = TextStyle(fontFamily = GhostFont, fontWeight = FontWeight.Bold,   fontSize = 40.sp, letterSpacing = 0.1.sp),
    titleLarge    = TextStyle(fontFamily = GhostFont, fontWeight = FontWeight.Bold,   fontSize = 28.sp, letterSpacing = 0.2.sp),
    titleMedium   = TextStyle(fontFamily = GhostFont, fontWeight = FontWeight.Medium, fontSize = 18.sp, letterSpacing = 0.15.sp),
    bodyLarge     = TextStyle(fontFamily = GhostFont, fontWeight = FontWeight.Normal, fontSize = 16.sp),
    bodyMedium    = TextStyle(fontFamily = GhostFont, fontWeight = FontWeight.Normal, fontSize = 14.sp),
    labelLarge    = TextStyle(fontFamily = GhostFont, fontWeight = FontWeight.Medium, fontSize = 14.sp, letterSpacing = 0.2.sp),
    labelMedium   = TextStyle(fontFamily = GhostFont, fontWeight = FontWeight.Normal, fontSize = 12.sp, letterSpacing = 0.2.sp),
)
