package com.localghost.app.qr

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Exercises the detection geometry (local binariser, finder detection, multi-triple selection and
 * perspective sampling) on synthetic frames, so the from-scratch detector has a runnable proof that
 * does not need a device.
 *
 * These tests drive the SAME entry point the live camera analyser calls, [QrSampler.sampleCandidates].
 * There is deliberately no test-only sampling path: a test that exercised different code than the app
 * would prove nothing about the app. The helper below only unwraps the candidate list (the live entry
 * returns several ranked candidates for the decoder to judge); it adds no logic.
 *
 * They render a QR-shaped module grid (three finder patterns + timing rows + a fixed data fill) into a
 * luminance buffer at a chosen scale, optionally with a brightness gradient (to test the local
 * binariser against glare), then assert the sampler recovers a grid of the right size. They do NOT
 * assert a decoded payload, because there is no app-side encoder to build a valid codeword stream; the
 * decode round-trip is covered separately. What these pin is that the geometry stages reach GridOk at
 * the correct module count, which is the part most likely to regress.
 */
class QrSamplerTest {

    /**
     * Call the live sampling entry and return its best candidate (or null) with the stage diagnostic.
     * This is the exact path [com.localghost.app.ui.QrScanScreen] runs per camera frame; the only thing
     * this helper does is take the top-ranked candidate from the returned list.
     */
    private fun liveSample(lum: IntArray, w: Int, h: Int): Pair<QrSampler.Sampled?, QrSampler.Diag> {
        val (candidates, diag) = QrSampler.sampleCandidates(lum, w, h)
        return candidates.firstOrNull() to diag
    }

    /** Build a v2 (25x25) module grid with valid finders, timing, and an alternating data fill. */
    private fun syntheticGrid(n: Int): Array<BooleanArray> {
        val g = Array(n) { BooleanArray(n) }
        fun finder(r0: Int, c0: Int) {
            for (dr in 0 until 7) for (dc in 0 until 7) {
                val edge = dr == 0 || dr == 6 || dc == 0 || dc == 6
                val core = dr in 2..4 && dc in 2..4
                g[r0 + dr][c0 + dc] = edge || core
            }
        }
        finder(0, 0); finder(0, n - 7); finder(n - 7, 0)
        // timing patterns
        for (i in 8 until n - 8) { g[6][i] = i % 2 == 0; g[i][6] = i % 2 == 0 }
        // a deterministic fill in the data region so binarisation has real structure to threshold
        for (r in 0 until n) for (c in 0 until n) {
            val inFinder = (r < 8 && c < 8) || (r < 8 && c >= n - 8) || (r >= n - 8 && c < 8)
            if (!inFinder && r != 6 && c != 6) g[r][c] = (r * 7 + c * 13) % 3 == 0
        }
        return g
    }

    /** Render a module grid to a luminance buffer: each module is scale x scale px, with a quiet zone. */
    private fun render(
        grid: Array<BooleanArray>, scale: Int, quiet: Int, gradient: Boolean,
    ): Triple<IntArray, Int, Int> {
        val n = grid.size
        val side = (n + quiet * 2) * scale
        val lum = IntArray(side * side) { 255 } // white background
        for (r in 0 until n) for (c in 0 until n) {
            if (!grid[r][c]) continue
            val y0 = (r + quiet) * scale; val x0 = (c + quiet) * scale
            for (yy in y0 until y0 + scale) for (xx in x0 until x0 + scale) lum[yy * side + xx] = 0
        }
        if (gradient) {
            // simulate glare: brighten one corner heavily so a global threshold would fail
            for (y in 0 until side) for (x in 0 until side) {
                val add = ((x + y).toDouble() / (2.0 * side) * 120).toInt()
                lum[y * side + x] = (lum[y * side + x] + add).coerceAtMost(255)
            }
        }
        return Triple(lum, side, side)
    }

    @Test fun recoversCleanGrid() {
        val n = 25
        val (lum, w, h) = render(syntheticGrid(n), scale = 6, quiet = 4, gradient = false)
        val (sampled, diag) = liveSample(lum, w, h)
        assertTrue("expected GridOk, got ${diag.note}", diag is QrSampler.Diag.GridOk)
        assertEquals(n, sampled!!.grid.size)
    }

    @Test fun recoversUnderGlareGradient() {
        // the local binariser must beat a brightness gradient a single global threshold cannot
        val n = 25
        val (lum, w, h) = render(syntheticGrid(n), scale = 6, quiet = 4, gradient = true)
        val (sampled, diag) = liveSample(lum, w, h)
        assertTrue("expected GridOk under glare, got ${diag.note}", diag is QrSampler.Diag.GridOk)
        assertEquals(n, sampled!!.grid.size)
    }

    @Test fun emptyFrameReportsCleanly() {
        val lum = IntArray(200 * 200) { 255 }
        val (sampled, diag) = liveSample(lum, 200, 200)
        assertTrue("blank frame should not find finders", sampled == null)
        assertTrue(diag is QrSampler.Diag.FewFinders || diag is QrSampler.Diag.NoBinary)
    }

    // ----------------------------------------------------------------------------------------------
    // Rotation: render a grid rotated by an arbitrary angle into a larger canvas, then check the
    // detector still finds the three finders and locks the right module count. Rotated finders are
    // what broke the real-camera path (the top-right finder's straight scan line was clipped), so the
    // diagonal cross-check must recover them.
    // ----------------------------------------------------------------------------------------------

    /** Render grid rotated by `deg` degrees about the canvas centre into a `canvas` x `canvas` frame. */
    private fun renderRotated(
        grid: Array<BooleanArray>, scale: Int, quiet: Int, deg: Double, canvas: Int,
    ): Triple<IntArray, Int, Int> {
        val n = grid.size
        val side = (n + quiet * 2) * scale
        val lum = IntArray(canvas * canvas) { 255 }
        val rad = Math.toRadians(deg)
        val cosA = Math.cos(rad); val sinA = Math.sin(rad)
        val cc = canvas / 2.0; val sc = side / 2.0
        // inverse map each canvas pixel back into the unrotated symbol bitmap
        for (y in 0 until canvas) {
            for (x in 0 until canvas) {
                val dx = x - cc; val dy = y - cc
                val sx = (cosA * dx + sinA * dy + sc)
                val sy = (-sinA * dx + cosA * dy + sc)
                val sxi = sx.toInt(); val syi = sy.toInt()
                if (sxi < 0 || sxi >= side || syi < 0 || syi >= side) continue
                val r = syi / scale - quiet; val c = sxi / scale - quiet
                val dark = r in 0 until n && c in 0 until n && grid[r][c]
                if (dark) lum[y * canvas + x] = 0
            }
        }
        return Triple(lum, canvas, canvas)
    }

    @Test fun recoversRotated10Degrees() {
        val n = 25
        val (lum, w, h) = renderRotated(syntheticGrid(n), scale = 7, quiet = 5, deg = 10.0, canvas = 480)
        val (sampled, diag) = liveSample(lum, w, h)
        assertTrue("expected GridOk at 10deg, got ${diag.note}", diag is QrSampler.Diag.GridOk)
        assertEquals(n, sampled!!.grid.size)
    }

    @Test fun recoversRotated20Degrees() {
        val n = 25
        val (lum, w, h) = renderRotated(syntheticGrid(n), scale = 7, quiet = 5, deg = 20.0, canvas = 520)
        val (sampled, diag) = liveSample(lum, w, h)
        assertTrue("expected GridOk at 20deg, got ${diag.note}", diag is QrSampler.Diag.GridOk)
        assertEquals(n, sampled!!.grid.size)
    }

    @Test fun recoversRotatedMinus15Degrees() {
        val n = 25
        val (lum, w, h) = renderRotated(syntheticGrid(n), scale = 7, quiet = 5, deg = -15.0, canvas = 500)
        val (sampled, diag) = liveSample(lum, w, h)
        assertTrue("expected GridOk at -15deg, got ${diag.note}", diag is QrSampler.Diag.GridOk)
        assertEquals(n, sampled!!.grid.size)
    }

    // ----------------------------------------------------------------------------------------------
    // Multiple QRs on screen: render two separate codes side by side. The detector should not be
    // confused into combining finders from different codes; it should lock a valid single grid (the
    // selection picks one self-consistent finder set, not a mix). This guards the real risk that a
    // second code's finder gets pulled into the first code's triangle.
    // ----------------------------------------------------------------------------------------------

    private fun renderTwo(
        gridA: Array<BooleanArray>, gridB: Array<BooleanArray>, scale: Int, quiet: Int, gap: Int,
    ): Triple<IntArray, Int, Int> {
        val nA = gridA.size; val nB = gridB.size
        val blkA = (nA + quiet * 2) * scale; val blkB = (nB + quiet * 2) * scale
        val w = blkA + gap + blkB
        val h = maxOf(blkA, blkB)
        val lum = IntArray(w * h) { 255 }
        fun blit(grid: Array<BooleanArray>, ox: Int) {
            val n = grid.size
            for (r in 0 until n) for (c in 0 until n) {
                if (!grid[r][c]) continue
                val y0 = (r + quiet) * scale; val x0 = ox + (c + quiet) * scale
                for (yy in y0 until y0 + scale) for (xx in x0 until x0 + scale) {
                    if (xx in 0 until w && yy in 0 until h) lum[yy * w + xx] = 0
                }
            }
        }
        blit(gridA, 0)
        blit(gridB, blkA + gap)
        return Triple(lum, w, h)
    }

    @Test fun twoCodesLockOneValidGrid() {
        // two different-size codes on screen; detector must lock exactly one self-consistent grid,
        // not a Frankengrid mixing finders from both.
        val a = syntheticGrid(21); val b = syntheticGrid(29)
        val (lum, w, h) = renderTwo(a, b, scale = 6, quiet = 4, gap = 60)
        val (sampled, diag) = liveSample(lum, w, h)
        assertTrue("expected a valid single grid, got ${diag.note}", diag is QrSampler.Diag.GridOk)
        // the locked grid must be one of the two real sizes, never a mix.
        assertTrue("locked size ${sampled!!.grid.size} not one of the two codes",
            sampled.grid.size == 21 || sampled.grid.size == 29)
    }

    @Test fun twoIdenticalCodesStillLock() {
        val a = syntheticGrid(25); val b = syntheticGrid(25)
        val (lum, w, h) = renderTwo(a, b, scale = 6, quiet = 4, gap = 80)
        val (sampled, diag) = liveSample(lum, w, h)
        assertTrue("expected GridOk with two identical codes, got ${diag.note}", diag is QrSampler.Diag.GridOk)
        assertEquals(25, sampled!!.grid.size)
    }

    // ----------------------------------------------------------------------------------------------
    // Robustness: a single finder pattern alone (e.g. a logo that looks like one) must NOT be read as
    // a QR. Fewer than three finders is a clean no-detect, not a false grid.
    // ----------------------------------------------------------------------------------------------

    @Test fun singleFinderIsNotAGrid() {
        val n = 25
        val g = Array(n) { BooleanArray(n) }
        for (dr in 0 until 7) for (dc in 0 until 7) {
            val edge = dr == 0 || dr == 6 || dc == 0 || dc == 6
            val core = dr in 2..4 && dc in 2..4
            g[dr][dc] = edge || core
        }
        val (lum, w, h) = render(g, scale = 8, quiet = 4, gradient = false)
        val (sampled, _) = liveSample(lum, w, h)
        assertTrue("one finder must not produce a grid", sampled == null)
    }
}
