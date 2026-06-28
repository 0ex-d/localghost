package com.localghost.app.qr

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Exercises the detection geometry (local binariser, finder detection, perspective sampling) on
 * synthetic frames, so the from-scratch detector has a runnable proof that does not need a device.
 *
 * These render a QR-shaped module grid (three finder patterns + timing rows + a fixed data fill) into
 * a luminance buffer at a chosen scale, optionally with a brightness gradient (to test the local
 * binariser against glare), then assert the sampler recovers a grid of the right size. They do NOT
 * assert a decoded payload, because there is no app-side encoder to build a valid codeword stream;
 * the decode round-trip is covered separately. What these pin is that the geometry stages reach
 * GridOk at the correct module count, which is the part most likely to regress.
 */
class QrSamplerTest {

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
        val (sampled, diag) = QrSampler.sampleDiag(lum, w, h)
        assertTrue("expected GridOk, got ${diag.note}", diag is QrSampler.Diag.GridOk)
        assertEquals(n, sampled!!.grid.size)
    }

    @Test fun recoversUnderGlareGradient() {
        // the local binariser must beat a brightness gradient a single global threshold cannot
        val n = 25
        val (lum, w, h) = render(syntheticGrid(n), scale = 6, quiet = 4, gradient = true)
        val (sampled, diag) = QrSampler.sampleDiag(lum, w, h)
        assertTrue("expected GridOk under glare, got ${diag.note}", diag is QrSampler.Diag.GridOk)
        assertEquals(n, sampled!!.grid.size)
    }

    @Test fun emptyFrameReportsCleanly() {
        val lum = IntArray(200 * 200) { 255 }
        val (sampled, diag) = QrSampler.sampleDiag(lum, 200, 200)
        assertTrue("blank frame should not find finders", sampled == null)
        assertTrue(diag is QrSampler.Diag.FewFinders || diag is QrSampler.Diag.NoBinary)
    }
}
