package com.localghost.app.ui

/**
 * WHICH ROWS THE DRILL-IN SHOWS. The box marks the rows a person wants first (what the daemon is
 * doing, what is waiting, what is wrong); the dialog shows those and folds the rest behind
 * "[ + N more ]". A box that marks nothing (an older build) still gets its first few rows shown,
 * never a blank card. Pure, so the JVM tests cover it.
 */
object DaemonRows {
    const val FALLBACK = 5

    /** One drill-in row; key marks the rows the box wants read first. */
    data class Row(val k: String, val v: String, val key: Boolean = false)

    data class Pick(val shown: List<Row>, val hidden: Int)

    fun pick(rows: List<Row>, all: Boolean): Pick {
        if (all || rows.isEmpty()) return Pick(rows, 0)
        val key = rows.filter { it.key }
        val shown = if (key.isNotEmpty()) key else rows.take(FALLBACK)
        return Pick(shown, rows.size - shown.size)
    }
}
