package com.localghost.app.ui

/**
 * PICKING ONE FIX ON THE MAP. Zoomed right in on the lit day, every fix it has a clock for is a
 * ring the person can tap, and a tapped fix can be deleted (with its neighbours at the same spot,
 * framed/forget.go on the box). Only that close: at a city's zoom a tap lands on the wrong fix, and
 * a delete is for good. Pure, so the tests read it.
 */
object MapPick {
    /** How close the map must be before a fix can be picked: a metre of ground a pixel or less
     *  (a phone's screen is then about a kilometre across). */
    const val PICK_M_PER_PX = 1.0

    /** From this close the map says a fix can be picked nearer in. */
    const val HINT_M_PER_PX = 12.0

    /** How far from a fix a tap still picks it, in dp. */
    const val TAP_DP = 28.0

    /** The deepest zoom (the map's zoom factor; px per unit is the short side / 1024 times this): a
     *  phone's screen is then about forty metres across at the equator, thirty on Paxos. */
    const val MAX_ZOOM = 1_000_000f

    /** The map's width in units (WorldRings' WORLD_UNITS, here too so this file stays pure). */
    const val UNITS = 1024.0

    /** Metres of ground per map unit at [lat] (Mercator: 1024 units round the equator). */
    fun metresPerUnit(lat: Double): Double =
        40_075_016.686 / UNITS * kotlin.math.cos(Math.toRadians(lat)).coerceAtLeast(1e-6)

    /** Metres of ground per screen pixel at [lat] with [pxPerUnit] screen px per map unit. */
    fun metresPerPx(lat: Double, pxPerUnit: Double): Double =
        if (pxPerUnit <= 0.0) Double.MAX_VALUE else metresPerUnit(lat) / pxPerUnit

    fun canPick(mPerPx: Double): Boolean = mPerPx <= PICK_M_PER_PX

    /**
     * The index of the fix nearest the tap, within [radiusPx], or -1. [xs]/[ys] are map units; the
     * camera is centred on ([cx], [cy]) with [pxPerUnit] px per unit on a [w]×[h] view. A fix
     * without a clock ([times] shorter than the fixes, or 0) cannot be picked: the box deletes by
     * the second.
     */
    fun nearest(xs: DoubleArray, ys: DoubleArray, times: LongArray, cx: Double, cy: Double, pxPerUnit: Double,
                w: Double, h: Double, tapX: Double, tapY: Double, radiusPx: Double): Int {
        if (times.size != xs.size || xs.size != ys.size) return -1
        var best = -1
        var bestD = radiusPx * radiusPx
        for (i in xs.indices) {
            if (times[i] <= 0L) continue
            val px = (xs[i] - cx) * pxPerUnit + w / 2
            val py = (ys[i] - cy) * pxPerUnit + h / 2
            val d = (px - tapX) * (px - tapX) + (py - tapY) * (py - tapY)
            if (d <= bestD) { bestD = d; best = i }
        }
        return best
    }

    /** What the card says about a fix and its run at the spot (the box's dry answer). */
    fun describe(ts: Long, run: LongArray?, clock: (Long) -> String): String = when {
        run == null -> "The fix at ${clock(ts)}."
        run.size <= 1 -> "The fix at ${clock(ts)}, on its own at this spot."
        else -> "The fix at ${clock(ts)}, and ${run.size - 1} more at this spot (${clock(run.first())} to ${clock(run.last())})."
    }

    /** Stay labels on the map claim their rectangle; one that would overlap a claimed one is left
     *  off (the ring still shows). [r] is left, top, right, bottom. */
    fun claim(claimed: MutableList<FloatArray>, r: FloatArray): Boolean {
        if (claimed.any { it[0] < r[2] && r[0] < it[2] && it[1] < r[3] && r[1] < it[3] }) return false
        claimed.add(r)
        return true
    }
}
