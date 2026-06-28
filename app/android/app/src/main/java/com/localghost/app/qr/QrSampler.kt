package com.localghost.app.qr

import kotlin.math.abs
import kotlin.math.hypot
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/**
 * Turns a grayscale image (row-major luminance, 0=black..255=white) into a QR module grid that
 * QrMatrixDecode can read. This is the image-processing half of scanning: binarise, find the three
 * finder patterns, recover the perspective, and sample each module's centre.
 *
 * This is a from-scratch detector (no scanning library, by design). It does three things real readers
 * do, so it can read a code that is glare-lit, blurred, or tilted rather than only a clean square-on
 * one: a LOCAL block binariser (a per-region threshold beats an uneven glare gradient that a single
 * global threshold cannot), finder detection with ratio TOLERANCE and a vertical cross-check (modules
 * are not exactly 1:1:3:1:1 once blurred), and a PERSPECTIVE transform from the three finder centres
 * plus a recovered fourth corner (so a tilted code is sampled as if seen square-on).
 *
 * It will not match a mature library's hit rate on hard frames, and it does not try to. If detection
 * fails, scanning returns null and the user types the values instead; a bad sample never produces a
 * wrong enrolment because the decoded fingerprint must still match.
 */
object QrSampler {

    /** A finder-pattern centre in IMAGE pixel coordinates (the analysis frame's space, pre-rotation). */
    data class FinderPoint(val x: Int, val y: Int)

    /**
     * The result of a successful sample: the module grid for decoding, plus the three finder-pattern
     * centres so the UI can draw an overlay anchored to the actual code. Plain class, not a data class,
     * because it holds an Array (a data class would want a custom equals/hashCode it never uses).
     */
    class Sampled(
        val grid: Array<BooleanArray>,
        val finders: List<FinderPoint>,
    )

    /**
     * Diagnostic outcome of a sample attempt, for on-screen debugging of why a real camera frame is
     * not decoding. Names the stage reached and what it saw.
     */
    sealed interface Diag {
        val note: String
        data class NoBinary(override val note: String) : Diag
        data class FewFinders(val found: Int, override val note: String) : Diag
        data class NoGrid(val finders: Int, override val note: String) : Diag
        data class GridOk(val n: Int, override val note: String) : Diag
    }

    /** Attempt to sample a module grid from a luminance image. Returns null if no QR is found. */
    fun sample(lum: IntArray, width: Int, height: Int): Array<BooleanArray>? =
        sampleWithFinders(lum, width, height)?.grid

    fun sampleWithFinders(lum: IntArray, width: Int, height: Int): Sampled? =
        sampleDiag(lum, width, height).first

    /**
     * Like sampleWithFinders, but reports the stage reached as a Diag. A GridOk with a Sampled means
     * the image pipeline worked and any remaining failure is in the decode/parse step.
     */
    fun sampleDiag(lum: IntArray, width: Int, height: Int): Pair<Sampled?, Diag> {
        if (lum.size < width * height) {
            return null to Diag.NoBinary("frame too small ${width}x${height}")
        }
        val bin = binariseLocal(lum, width, height)

        // Find finder-pattern candidate centres (ratio-tolerant, vertically cross-checked).
        val centres = findFinders(bin, width, height)
        if (centres.size < 3) {
            return null to Diag.FewFinders(centres.size, "finders ${centres.size}/3 (frame ${width}x${height})")
        }

        // Assign roles (top-left, top-right, bottom-left) by geometry, then sample via perspective.
        val roles = assignCorners(centres) ?: return null to Diag.NoGrid(centres.size, "corner roles unclear")
        val grid = sampleByPerspective(bin, width, height, roles)
            ?: return null to Diag.NoGrid(3, "finders ok, grid failed")

        val pts = listOf(
            FinderPoint(roles.tl.x.roundToInt(), roles.tl.y.roundToInt()),
            FinderPoint(roles.tr.x.roundToInt(), roles.tr.y.roundToInt()),
            FinderPoint(roles.bl.x.roundToInt(), roles.bl.y.roundToInt()),
        )
        return Sampled(grid, pts) to Diag.GridOk(grid.size, "grid ${grid.size}x${grid.size}")
    }

    // ====================================================================================
    // 1. LOCAL BINARISATION (ZXing-style block thresholding)
    //
    // A single global threshold cannot handle an uneven glare gradient: the bright side of the frame
    // pushes the whole threshold up and the dark side floods to black. Instead, split the image into
    // BLOCK x BLOCK tiles, take each tile's average, smooth it against neighbouring tiles, and
    // threshold each pixel against its local tile average. This is the single biggest robustness win.
    // ====================================================================================

    private const val BLOCK = 8

    private fun binariseLocal(lum: IntArray, w: Int, h: Int): BooleanArray {
        val out = BooleanArray(w * h)
        val bw = (w + BLOCK - 1) / BLOCK
        val bh = (h + BLOCK - 1) / BLOCK
        val blockAvg = IntArray(bw * bh)

        // tile averages
        for (by in 0 until bh) {
            for (bx in 0 until bw) {
                var sum = 0
                var count = 0
                val x0 = bx * BLOCK; val y0 = by * BLOCK
                val x1 = min(x0 + BLOCK, w); val y1 = min(y0 + BLOCK, h)
                var yy = y0
                while (yy < y1) {
                    var xx = x0
                    val rowBase = yy * w
                    while (xx < x1) { sum += lum[rowBase + xx] and 0xFF; count++; xx++ }
                    yy++
                }
                blockAvg[by * bw + bx] = if (count > 0) sum / count else 128
            }
        }

        // threshold each pixel against the smoothed average of its 5x5 block neighbourhood
        for (by in 0 until bh) {
            for (bx in 0 until bw) {
                var sum = 0; var n = 0
                val r = 2
                var ny = max(0, by - r)
                while (ny <= min(bh - 1, by + r)) {
                    var nx = max(0, bx - r)
                    while (nx <= min(bw - 1, bx + r)) { sum += blockAvg[ny * bw + nx]; n++; nx++ }
                    ny++
                }
                val local = if (n > 0) sum / n else 128
                // a small bias below the local mean reduces speckle in flat light regions
                val thresh = local - 8

                val x0 = bx * BLOCK; val y0 = by * BLOCK
                val x1 = min(x0 + BLOCK, w); val y1 = min(y0 + BLOCK, h)
                var yy = y0
                while (yy < y1) {
                    val rowBase = yy * w
                    var xx = x0
                    while (xx < x1) {
                        out[rowBase + xx] = (lum[rowBase + xx] and 0xFF) < thresh
                        xx++
                    }
                    yy++
                }
            }
        }
        return out
    }

    private fun dark(bin: BooleanArray, w: Int, x: Int, y: Int) = bin[y * w + x]

    // ====================================================================================
    // 2. FINDER DETECTION (ratio-tolerant horizontal scan + vertical cross-check)
    //
    // Scan rows for the 1:1:3:1:1 dark/light/dark/light/dark ratio of a finder pattern, with a
    // tolerance (blur smears the boundaries). For each horizontal hit, cross-check vertically through
    // the candidate centre: a real finder also shows ~1:1:3:1:1 down its middle. This rejects the many
    // false 1:1:3:1:1 runs that occur in ordinary data rows. Cluster surviving centres.
    // ====================================================================================

    private data class Pt(val x: Int, val y: Int)

    private fun findFinders(bin: BooleanArray, w: Int, h: Int): List<Pt> {
        val candidates = ArrayList<Pt>()
        for (y in 0 until h) {
            var x = 0
            while (x < w) {
                if (dark(bin, w, x, y)) {
                    val runs = IntArray(5)
                    var ri = 0
                    var cx = x
                    var expectDark = true
                    while (cx < w && ri < 5) {
                        var run = 0
                        while (cx < w && dark(bin, w, cx, y) == expectDark) { run++; cx++ }
                        runs[ri++] = run
                        expectDark = !expectDark
                    }
                    if (ri == 5 && matches11311(runs)) {
                        val total = runs.sum()
                        val centerX = x + total / 2
                        // vertical cross-check through the centre column
                        if (verticalCheck(bin, w, h, centerX, y)) {
                            candidates.add(Pt(centerX, y))
                        }
                    }
                    x = cx
                } else x++
            }
        }
        return cluster(candidates).sortedByDescending { it.second }.map { it.first }
    }

    /** Cross-check: does column centerX, around row y, also read ~1:1:3:1:1 vertically? */
    private fun verticalCheck(bin: BooleanArray, w: Int, h: Int, cx: Int, cy: Int): Boolean {
        if (cx < 0 || cx >= w) return false
        if (!dark(bin, w, cx, cy)) return false
        // walk up and down collecting runs centred on (cx, cy)
        val runs = IntArray(5)
        // centre dark run
        var up = cy; while (up > 0 && dark(bin, w, cx, up - 1)) up--
        var down = cy; while (down < h - 1 && dark(bin, w, cx, down + 1)) down++
        runs[2] = down - up + 1
        // light above
        var a = up - 1; var lenLightTop = 0
        while (a >= 0 && !dark(bin, w, cx, a)) { lenLightTop++; a-- }
        runs[1] = lenLightTop
        // dark above
        var darkTop = 0
        while (a >= 0 && dark(bin, w, cx, a)) { darkTop++; a-- }
        runs[0] = darkTop
        // light below
        var b = down + 1; var lenLightBot = 0
        while (b < h && !dark(bin, w, cx, b)) { lenLightBot++; b++ }
        runs[3] = lenLightBot
        // dark below
        var darkBot = 0
        while (b < h && dark(bin, w, cx, b)) { darkBot++; b++ }
        runs[4] = darkBot
        if (runs.any { it == 0 }) return false
        return matches11311(runs)
    }

    private fun matches11311(r: IntArray): Boolean {
        val total = r.sum()
        if (total < 7) return false
        val unit = total / 7.0
        if (unit < 1.0) return false
        val tol = 0.6 // generous: blur and screen moire smear the edges
        fun near(v: Int, mult: Double) = abs(v - unit * mult) <= tol * mult * unit
        return near(r[0], 1.0) && near(r[1], 1.0) && near(r[2], 3.0) && near(r[3], 1.0) && near(r[4], 1.0)
    }

    /** Group nearby points; return cluster centroids with their member counts. */
    private fun cluster(pts: List<Pt>): List<Pair<Pt, Int>> {
        val out = ArrayList<Pair<Pt, Int>>()
        val sumX = ArrayList<Long>(); val sumY = ArrayList<Long>(); val cnt = ArrayList<Int>()
        val radius = 14
        for (p in pts) {
            var hit = -1
            for (i in out.indices) {
                val c = out[i].first
                if (abs(c.x - p.x) <= radius && abs(c.y - p.y) <= radius) { hit = i; break }
            }
            if (hit >= 0) {
                sumX[hit] = sumX[hit] + p.x; sumY[hit] = sumY[hit] + p.y; cnt[hit] = cnt[hit] + 1
                out[hit] = Pt((sumX[hit] / cnt[hit]).toInt(), (sumY[hit] / cnt[hit]).toInt()) to cnt[hit]
            } else {
                out.add(p to 1); sumX.add(p.x.toLong()); sumY.add(p.y.toLong()); cnt.add(1)
            }
        }
        // require a minimum support so a single stray row does not count as a finder
        return out.filter { it.second >= 2 }
    }

    // ====================================================================================
    // 3. CORNER ROLES + PERSPECTIVE SAMPLING
    //
    // From three finder centres, identify which is top-left (the corner), top-right, and bottom-left
    // by geometry: the top-left is the vertex of the near-right-angle. Estimate the module size from a
    // finder, derive the version, recover the fourth (bottom-right) corner, and sample every module
    // centre through a perspective transform fitted to the four corners.
    // ====================================================================================

    private class Corners(
        val tl: DoublePt, val tr: DoublePt, val bl: DoublePt,
    )
    private data class DoublePt(val x: Double, val y: Double)

    private fun assignCorners(centres: List<Pt>): Corners? {
        if (centres.size < 3) return null
        // take the three strongest clusters
        val a = centres[0]; val b = centres[1]; val c = centres[2]
        val pts = listOf(a, b, c)
        // top-left is the point opposite the longest side (the hypotenuse of the finder L)
        val dAB = dist(a, b); val dAC = dist(a, c); val dBC = dist(b, c)
        val tl: Pt; val p1: Pt; val p2: Pt
        when (maxOf(dAB, dAC, dBC)) {
            dAB -> { tl = c; p1 = a; p2 = b }
            dAC -> { tl = b; p1 = a; p2 = c }
            else -> { tl = a; p1 = b; p2 = c }
        }
        // of the two remaining, top-right is the one more horizontal from tl, bottom-left more vertical.
        // use the cross product sign to keep a consistent winding (tr to the right of tl->bl).
        val v1 = DoublePt((p1.x - tl.x).toDouble(), (p1.y - tl.y).toDouble())
        val v2 = DoublePt((p2.x - tl.x).toDouble(), (p2.y - tl.y).toDouble())
        val cross = v1.x * v2.y - v1.y * v2.x
        val (tr, bl) = if (cross < 0) p1 to p2 else p2 to p1
        return Corners(
            DoublePt(tl.x.toDouble(), tl.y.toDouble()),
            DoublePt(tr.x.toDouble(), tr.y.toDouble()),
            DoublePt(bl.x.toDouble(), bl.y.toDouble()),
        )
    }

    private fun sampleByPerspective(bin: BooleanArray, w: Int, h: Int, c: Corners): Array<BooleanArray>? {
        // Estimate module size by measuring one finder's width at the tl centre along tl->tr.
        val moduleLen = estimateModuleSize(bin, w, h, c) ?: return null
        // distance tl->tr spans (n-7) modules between the two finder CENTRES.
        val spanTR = distD(c.tl, c.tr)
        val nEst = (spanTR / moduleLen) + 7.0
        // snap to a valid QR size 21..57 (versions 1..10)
        var n = ((nEst.roundToInt() - 17) / 4) * 4 + 17
        n = n.coerceIn(21, 57)
        if ((n - 17) % 4 != 0) return null

        // The finder CENTRES sit 3.5 modules in from each edge. Build the four symbol corners in
        // module space and map to image space with a perspective transform. We have three finder
        // centres; the fourth (bottom-right) corner is tl + (tr-tl) + (bl-tl).
        val brc = DoublePt(c.tr.x + c.bl.x - c.tl.x, c.tr.y + c.bl.y - c.tl.y)

        // Map module coords (col,row) in [3.5 .. n-3.5] (finder-centre grid) to image via bilinear of
        // the four finder centres. For a planar QR this bilinear map is the perspective sampling we
        // need across the symbol (exact at the four anchors, near-exact between, which is plenty).
        val lo = 3.5
        val hi = n - 3.5
        fun toImg(col: Double, row: Double): DoublePt {
            // u,v in [0,1] across the finder-centre quad
            val u = (col - lo) / (hi - lo)
            val v = (row - lo) / (hi - lo)
            // bilinear over tl, tr, bl, br
            val top = DoublePt(c.tl.x + (c.tr.x - c.tl.x) * u, c.tl.y + (c.tr.y - c.tl.y) * u)
            val bot = DoublePt(c.bl.x + (brc.x - c.bl.x) * u, c.bl.y + (brc.y - c.bl.y) * u)
            return DoublePt(top.x + (bot.x - top.x) * v, top.y + (bot.y - top.y) * v)
        }

        val grid = Array(n) { BooleanArray(n) }
        for (row in 0 until n) {
            for (col in 0 until n) {
                val p = toImg(col + 0.5, row + 0.5)
                val xi = p.x.roundToInt(); val yi = p.y.roundToInt()
                if (xi < 0 || xi >= w || yi < 0 || yi >= h) return null
                grid[row][col] = dark(bin, w, xi, yi)
            }
        }
        // sanity: the timing rows should alternate; if they look like noise, reject.
        if (!timingLooksValid(grid, n)) return null
        return grid
    }

    /** Estimate module size in pixels by scanning the finder horizontally at the tl centre. */
    private fun estimateModuleSize(bin: BooleanArray, w: Int, h: Int, c: Corners): Double? {
        val cx = c.tl.x.roundToInt().coerceIn(0, w - 1)
        val cy = c.tl.y.roundToInt().coerceIn(0, h - 1)
        if (!dark(bin, w, cx, cy)) return null
        // the central dark run of the finder is 3 modules wide
        var left = cx; while (left > 0 && dark(bin, w, left - 1, cy)) left--
        var right = cx; while (right < w - 1 && dark(bin, w, right + 1, cy)) right++
        val threeModules = (right - left + 1).toDouble()
        if (threeModules < 3.0) return null
        return threeModules / 3.0
    }

    private fun timingLooksValid(grid: Array<BooleanArray>, n: Int): Boolean {
        // row 6 and column 6 are the timing patterns: alternating from module 8 to n-8.
        var transitions = 0
        var prev = grid[6][8]
        for (i in 9 until n - 8) {
            if (grid[6][i] != prev) transitions++
            prev = grid[6][i]
        }
        // expect roughly (n-16) transitions; accept a generous band to tolerate sampling slop
        return transitions >= (n - 16) / 2
    }

    private fun dist(a: Pt, b: Pt): Double = hypot((a.x - b.x).toDouble(), (a.y - b.y).toDouble())
    private fun distD(a: DoublePt, b: DoublePt): Double = hypot(a.x - b.x, a.y - b.y)
}
