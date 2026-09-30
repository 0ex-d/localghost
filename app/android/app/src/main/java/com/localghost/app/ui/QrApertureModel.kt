package com.localghost.app.ui

/**
 * The vault aperture the QR scanner draws over the code, in the same language as the unlock rings.
 * Pure, so the counts and angles are tested; [QrScanScreen] draws them.
 *
 * A ring of segments sits around the code. As the box's rotating enrolment frames land, the segments
 * light one by one; when enough have (any K of K+M, erasure-coded), the ring is full and the iris
 * opens. A single clean enrol code fills the ring in one go. A code that reads but is not the way in
 * turns the ring red. No ghosts, no fireworks , the lock either opens or it does not.
 */
object QrApertureModel {
    /** Segments around the aperture. Twelve reads clearly at a phone's arm length. */
    const val SEGMENTS = 12

    /**
     * How many segments are lit for a capture of [have] of [want] frames. [want] <= 1 (a single
     * clean code) lights all of them at once on the first frame. Always at least one while any
     * frame is in, and never more than [SEGMENTS].
     */
    fun litSegments(have: Int, want: Int, segments: Int = SEGMENTS): Int {
        if (have <= 0) return 0
        if (want <= 1) return segments
        val frac = have.toFloat() / want
        return (frac * segments).toInt().coerceIn(1, segments)
    }

    /** The capture as a fraction 0..1, for the iris and the fill. */
    fun progress(have: Int, want: Int): Float =
        if (want <= 1) (if (have >= 1) 1f else 0f) else (have.toFloat() / want).coerceIn(0f, 1f)

    /** Angle of segment [i] of [segments] around the ring, degrees, 0 at the top, clockwise. */
    fun segmentAngle(i: Int, segments: Int = SEGMENTS): Float = -90f + i * (360f / segments)

    /** How far the iris is open (0 shut, 1 wide) at [ms] into an open of [openMs], eased. */
    fun irisOpen(ms: Long, openMs: Long): Float {
        if (openMs <= 0) return 1f
        val t = (ms.toFloat() / openMs).coerceIn(0f, 1f)
        return 1f - (1f - t) * (1f - t) // ease-out
    }

    /** What the aperture is doing, for the drawing to switch on. */
    enum class Phase { HUNTING, LOCKING, READING, WRONG, FOUND }

    /**
     * The phase from what the scanner sees this frame: a code locked on or not, how many frames are
     * in, whether the code read but is not ours, and whether enrolment has latched.
     */
    fun phase(codeInView: Boolean, framesHave: Int, wrong: Boolean, found: Boolean): Phase = when {
        found -> Phase.FOUND
        wrong -> Phase.WRONG
        framesHave > 0 -> Phase.READING
        codeInView -> Phase.LOCKING
        else -> Phase.HUNTING
    }
}
