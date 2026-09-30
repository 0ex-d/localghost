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
    /** Segments around the aiming reticle: eight, one for each frame the box's rotating enrolment
     *  needs (any eight of its twelve). A clean single code lights all eight at once. */
    const val SEGMENTS = 8

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

    // --- establishing identity: the sequence played once a box is found, told in animation ---
    //
    // Enrolment is the phone gaining a signed identity with the box. Rather than an iris "opening",
    // the found screen walks four steps that build the identity out, then hands over to the PIN. Pure,
    // so the step at a given moment and its progress are tested; the screen draws each step's glyph.

    /** The steps of the establishing sequence, in order. READY is the arrival, not a step. */
    enum class Step { IDENTITY, CHANNEL, CERTIFICATE, PINNED }

    /** The fraction of the sequence spent on the steps; the rest holds on READY. */
    const val STEPS_FRAC = 0.82f

    /** The step at [t] (0..1 over the whole sequence), or null once it is READY. */
    fun stepAt(t: Float): Step? {
        if (t >= STEPS_FRAC) return null
        val n = Step.entries.size
        val i = (t / STEPS_FRAC * n).toInt().coerceIn(0, n - 1)
        return Step.entries[i]
    }

    /** How far through its own step [t] is (0..1); 1 once READY. */
    fun stepProgress(t: Float): Float {
        if (t >= STEPS_FRAC) return 1f
        val n = Step.entries.size
        val each = STEPS_FRAC / n
        val i = (t / each).toInt().coerceIn(0, n - 1)
        return ((t - i * each) / each).coerceIn(0f, 1f)
    }

    /** How many steps are fully done at [t] , for the beads that fill as the identity builds out. */
    fun stepsDone(t: Float): Int {
        val n = Step.entries.size
        if (t >= STEPS_FRAC) return n
        return (t / STEPS_FRAC * n).toInt().coerceIn(0, n)
    }

    /** READY: the identity is established, waiting on the PIN. */
    fun ready(t: Float): Boolean = t >= STEPS_FRAC
}
