package com.localghost.app.ui

import java.util.Locale

/** The MAP's words that are not its drawing. Pure, so the JVM tests read it. */
object MapText {
    /** "climbed 420 m · highest 1,240 m up", from the box's elevation tiles. */
    fun heights(climbM: Double, highM: Double): String = listOf(
        if (climbM >= 20) "climbed " + "%,d".format(Locale.UK, Math.round(climbM)) + " m" else "",
        if (highM > 0) "highest " + "%,d".format(Locale.UK, Math.round(highM)) + " m up" else "",
    ).filter { it.isNotEmpty() }.joinToString(" · ")
}
