package com.localghost.app.qr

/**
 * Turns a grayscale image (row-major luminance, 0=black..255=white) into a QR module grid that
 * QrMatrixDecode can read. This is the image-processing half of scanning: binarise, find the three
 * finder patterns, establish the grid, and sample each module's centre.
 *
 * Scope: the enrolment QR is a crisp, high-contrast symbol shown on a screen or printed, scanned
 * roughly square-on (the user points the phone at the box's terminal). So this does light-weight
 * detection , global-ish thresholding and axis-aligned grid sampling , rather than full perspective
 * recovery. If detection fails, scanning simply returns null and the user types the values instead;
 * a bad sample never produces a wrong enrolment because the decoded fingerprint must still match.
 */
object QrSampler {

    /** Attempt to sample a module grid from a luminance image. Returns null if no QR is found. */
    fun sample(lum: IntArray, width: Int, height: Int): Array<BooleanArray>? {
        val bin = binarise(lum, width, height) ?: return null
        val finders = findFinderCenters(bin, width, height) ?: return null
        return buildGrid(bin, width, height, finders)
    }

    // --- binarisation (Otsu global threshold; fine for high-contrast QR) ---

    private fun binarise(lum: IntArray, w: Int, h: Int): BooleanArray? {
        if (lum.size < w * h) return null
        val hist = IntArray(256)
        for (i in 0 until w * h) hist[lum[i] and 0xFF]++
        val total = w * h
        var sum = 0.0
        for (t in 0..255) sum += t.toDouble() * hist[t]
        var sumB = 0.0; var wB = 0; var maxVar = -1.0; var thresh = 127
        for (t in 0..255) {
            wB += hist[t]; if (wB == 0) continue
            val wF = total - wB; if (wF == 0) break
            sumB += t.toDouble() * hist[t]
            val mB = sumB / wB; val mF = (sum - sumB) / wF
            val between = wB.toDouble() * wF * (mB - mF) * (mB - mF)
            if (between > maxVar) { maxVar = between; thresh = t }
        }
        val out = BooleanArray(w * h)
        for (i in 0 until w * h) out[i] = (lum[i] and 0xFF) < thresh // true = dark module
        return out
    }

    private fun dark(bin: BooleanArray, w: Int, x: Int, y: Int) = bin[y * w + x]

    // --- finder detection: scan rows for the 1:1:3:1:1 dark/light ratio ---

    private data class Pt(val x: Int, val y: Int)

    private fun findFinderCenters(bin: BooleanArray, w: Int, h: Int): Triple<Pt, Pt, Pt>? {
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
                        val center = x + total / 2
                        candidates.add(Pt(center, y))
                    }
                    x = cx
                } else x++
            }
        }
        if (candidates.size < 3) return null
        // cluster candidates and take the three densest clusters as finder centres
        val clusters = cluster(candidates)
        if (clusters.size < 3) return null
        clusters.sortByDescending { it.second }
        val c = clusters.take(3).map { it.first }
        return Triple(c[0], c[1], c[2])
    }

    private fun matches11311(r: IntArray): Boolean {
        val unit = (r[0] + r[1] + r[3] + r[4] + r[2] / 3.0) / 7.0
        if (unit < 1) return false
        val tol = unit * 0.6
        fun near(v: Int, mult: Double) = kotlin.math.abs(v - unit * mult) <= tol * mult
        return near(r[0], 1.0) && near(r[1], 1.0) && near(r[2], 3.0) && near(r[3], 1.0) && near(r[4], 1.0)
    }

    private fun cluster(pts: List<Pt>): MutableList<Pair<Pt, Int>> {
        val out = ArrayList<Pair<Pt, Int>>()
        val used = BooleanArray(pts.size)
        for (i in pts.indices) {
            if (used[i]) continue
            var sx = pts[i].x; var sy = pts[i].y; var count = 1; used[i] = true
            for (j in i + 1 until pts.size) {
                if (used[j]) continue
                if (kotlin.math.abs(pts[j].x - pts[i].x) < 20 && kotlin.math.abs(pts[j].y - pts[i].y) < 20) {
                    sx += pts[j].x; sy += pts[j].y; count++; used[j] = true
                }
            }
            out.add(Pair(Pt(sx / count, sy / count), count))
        }
        return out
    }

    // --- grid sampling: from three finder centres, establish axes and sample module centres ---

    private fun buildGrid(bin: BooleanArray, w: Int, h: Int, f: Triple<Pt, Pt, Pt>): Array<BooleanArray>? {
        // Identify the corner (top-left) finder as the one whose centre is closest to the other two
        // forming a right angle. For a near-square scan we approximate: top-left has min(x+y).
        val pts = listOf(f.first, f.second, f.third)
        val tl = pts.minByOrNull { it.x + it.y }!!
        val tr = pts.filter { it != tl }.maxByOrNull { it.x - it.y }!!
        val bl = pts.first { it != tl && it != tr }

        // Estimate module size from finder spacing. Distance tl->tr spans (n-7) modules between
        // finder centres. We do not know n yet, so estimate module size from the finder width by a
        // second pass: sample the run length at tl. Simpler: derive n by trying the supported sizes
        // and picking the one whose sampled timing pattern alternates cleanly.
        for (v in 1..10) {
            val n = 17 + 4 * v
            val grid = trySampleAtVersion(bin, w, h, tl, tr, bl, n) ?: continue
            if (timingLooksValid(grid, n)) return grid
        }
        return null
    }

    private fun trySampleAtVersion(bin: BooleanArray, w: Int, h: Int, tl: Pt, tr: Pt, bl: Pt, n: Int): Array<BooleanArray>? {
        // Map module (col,row) -> image point by bilinear interpolation of the three finder centres.
        // Finder centres sit at module (3,3), (n-4,3), (3,n-4).
        val cols = n; val rows = n
        fun img(col: Double, row: Double): Pt {
            val ax = (col - 3.0) / (n - 4 - 3.0) // 0 at tl col .. 1 at tr col
            val ay = (row - 3.0) / (n - 4 - 3.0)
            val x = tl.x + ax * (tr.x - tl.x) + ay * (bl.x - tl.x)
            val y = tl.y + ax * (tr.y - tl.y) + ay * (bl.y - tl.y)
            return Pt(x.toInt(), y.toInt())
        }
        val grid = Array(rows) { BooleanArray(cols) }
        for (r in 0 until rows) for (c in 0 until cols) {
            val p = img(c.toDouble(), r.toDouble())
            if (p.x < 0 || p.x >= w || p.y < 0 || p.y >= h) return null
            grid[r][c] = dark(bin, w, p.x, p.y)
        }
        return grid
    }

    private fun timingLooksValid(grid: Array<BooleanArray>, n: Int): Boolean {
        // The timing row (row 6) and column (col 6) alternate dark/light between the finders.
        var alt = 0; var tot = 0
        for (c in 8 until n - 8) {
            val expect = c % 2 == 0
            if (grid[6][c] == expect) alt++
            tot++
        }
        return tot > 0 && alt.toDouble() / tot > 0.8
    }
}
