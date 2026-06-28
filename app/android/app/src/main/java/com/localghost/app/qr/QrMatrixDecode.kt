package com.localghost.app.qr

/**
 * Decodes a QR module matrix (already sampled to a square boolean grid, true = dark) back to its
 * byte-mode text. This is the inverse of the box's from-scratch encoder, restricted to the same
 * subset: byte mode, versions 1-10, EC level M, which is all the enrolment QR ever uses. It reuses
 * the Reed-Solomon error correction already in this package.
 *
 * It does NOT do image processing: turning a camera frame into this boolean grid (finder detection,
 * sampling) is the QrScanner's job. Keeping the matrix decode separate makes it testable against the
 * encoder without a camera.
 */
object QrMatrixDecode {

    class DecodeError(msg: String) : Exception(msg)

    // versionM[v] = ecPerBlock, g1blocks, g1dataCW, g2blocks, g2dataCW  (level M; matches encoder)
    private val versionM = mapOf(
        1 to intArrayOf(10, 1, 16, 0, 0),
        2 to intArrayOf(16, 1, 28, 0, 0),
        3 to intArrayOf(26, 1, 44, 0, 0),
        4 to intArrayOf(18, 2, 32, 0, 0),
        5 to intArrayOf(24, 2, 43, 0, 0),
        6 to intArrayOf(16, 4, 27, 0, 0),
        7 to intArrayOf(18, 4, 31, 0, 0),
        8 to intArrayOf(22, 2, 38, 2, 39),
        9 to intArrayOf(22, 3, 36, 2, 37),
        10 to intArrayOf(26, 4, 43, 1, 44),
    )

    private val alignCenters = mapOf(
        1 to intArrayOf(), 2 to intArrayOf(6, 18), 3 to intArrayOf(6, 22), 4 to intArrayOf(6, 26),
        5 to intArrayOf(6, 30), 6 to intArrayOf(6, 34), 7 to intArrayOf(6, 22, 38),
        8 to intArrayOf(6, 24, 42), 9 to intArrayOf(6, 26, 46), 10 to intArrayOf(6, 28, 50),
    )

    private val maskFns = arrayOf<(Int, Int) -> Boolean>(
        { r, c -> (r + c) % 2 == 0 },
        { r, _ -> r % 2 == 0 },
        { _, c -> c % 3 == 0 },
        { r, c -> (r + c) % 3 == 0 },
        { r, c -> (r / 2 + c / 3) % 2 == 0 },
        { r, c -> (r * c) % 2 + (r * c) % 3 == 0 },
        { r, c -> ((r * c) % 2 + (r * c) % 3) % 2 == 0 },
        { r, c -> ((r + c) % 2 + (r * c) % 3) % 2 == 0 },
    )

    /** Decode a square module grid to text. Throws DecodeError on any inconsistency. */
    fun decode(grid: Array<BooleanArray>): String {
        val n = grid.size
        if (n < 21 || n > 57 || (n - 17) % 4 != 0) throw DecodeError("not a valid module count: $n")
        // defence in depth: the grid must be square (every row n wide). The sampler builds square
        // grids, but decode also runs on adversarial input, and a jagged grid would throw an
        // unpredictable index error deep in the readers. Reject it cleanly here instead.
        for (row in grid) if (row.size != n) throw DecodeError("jagged grid")
        val v = (n - 17) / 4
        if (v !in 1..10) throw DecodeError("unsupported version $v")

        val mask = readFormatMask(grid, n)
        val reserved = reservedMatrix(v, n)
        val codewords = readCodewords(grid, reserved, n, mask)
        val data = correctAndAssemble(codewords, v)
        return parseByteMode(data, v)
    }

    private fun readFormatMask(grid: Array<BooleanArray>, n: Int): Int {
        // Read the 15 format bits near the top-left, unmask with 0x5412, extract the 3 mask bits.
        // We read the copy along row 8 / column 8.
        val coords = arrayOf(
            intArrayOf(8, 0), intArrayOf(8, 1), intArrayOf(8, 2), intArrayOf(8, 3), intArrayOf(8, 4),
            intArrayOf(8, 5), intArrayOf(8, 7), intArrayOf(8, 8), intArrayOf(7, 8), intArrayOf(5, 8),
            intArrayOf(4, 8), intArrayOf(3, 8), intArrayOf(2, 8), intArrayOf(1, 8), intArrayOf(0, 8),
        )
        var bits = 0
        for (rc in coords) bits = (bits shl 1) or if (grid[rc[0]][rc[1]]) 1 else 0
        val unmasked = bits xor 0x5412
        // format = 5 bits (2 EC level + 3 mask) in the top, followed by 10 BCH. Extract mask bits.
        val data5 = (unmasked shr 10) and 0x1F
        return data5 and 0x07
    }

    private fun reservedMatrix(v: Int, n: Int): Array<BooleanArray> {
        val res = Array(n) { BooleanArray(n) }
        fun reserveFinder(r: Int, c: Int) {
            for (dr in -1..7) for (dc in -1..7) {
                val rr = r + dr; val cc = c + dc
                if (rr in 0 until n && cc in 0 until n) res[rr][cc] = true
            }
        }
        reserveFinder(0, 0); reserveFinder(0, n - 7); reserveFinder(n - 7, 0)
        for (i in 8 until n - 8) { res[6][i] = true; res[i][6] = true }
        res[4 * v + 9][8] = true
        val centers = alignCenters[v]!!
        for (r in centers) for (c in centers) {
            if ((r <= 8 && c <= 8) || (r <= 8 && c >= n - 9) || (r >= n - 9 && c <= 8)) continue
            for (dr in -2..2) for (dc in -2..2) res[r + dr][c + dc] = true
        }
        for (i in 0..8) { res[8][i] = true; res[i][8] = true }
        for (i in 0 until 8) { res[8][n - 1 - i] = true; res[n - 1 - i][8] = true }
        return res
    }

    private fun readCodewords(grid: Array<BooleanArray>, res: Array<BooleanArray>, n: Int, mask: Int): IntArray {
        val bits = ArrayList<Int>()
        var col = n - 1
        var upward = true
        while (col > 0) {
            if (col == 6) col--
            for (i in 0 until n) {
                val r = if (upward) n - 1 - i else i
                for (c in intArrayOf(col, col - 1)) {
                    if (!res[r][c]) {
                        var bit = if (grid[r][c]) 1 else 0
                        if (maskFns[mask](r, c)) bit = bit xor 1
                        bits.add(bit)
                    }
                }
            }
            col -= 2
            upward = !upward
        }
        val cws = ArrayList<Int>()
        var i = 0
        while (i + 8 <= bits.size) {
            var v = 0
            for (j in 0 until 8) v = (v shl 1) or bits[i + j]
            cws.add(v)
            i += 8
        }
        return cws.toIntArray()
    }

    private fun correctAndAssemble(cws: IntArray, v: Int): IntArray {
        val cfg = versionM[v]!!
        val ecpb = cfg[0]; val g1b = cfg[1]; val g1d = cfg[2]; val g2b = cfg[3]; val g2d = cfg[4]
        val sizes = ArrayList<Int>()
        repeat(g1b) { sizes.add(g1d) }
        repeat(g2b) { sizes.add(g2d) }
        val numBlocks = sizes.size
        // de-interleave data codewords
        val dataBlocks = Array(numBlocks) { ArrayList<Int>() }
        var idx = 0
        val maxd = sizes.max()
        for (k in 0 until maxd) {
            for (bi in 0 until numBlocks) if (k < sizes[bi]) { dataBlocks[bi].add(cws[idx]); idx++ }
        }
        // de-interleave ec codewords
        val ecBlocks = Array(numBlocks) { ArrayList<Int>() }
        for (k in 0 until ecpb) {
            for (bi in 0 until numBlocks) { ecBlocks[bi].add(cws[idx]); idx++ }
        }
        // RS-correct each block (data + ec) and keep the data portion
        val out = ArrayList<Int>()
        for (bi in 0 until numBlocks) {
            val full = (dataBlocks[bi] + ecBlocks[bi]).toIntArray()
            val corrected = ReedSolomon.decode(full, ecpb)
                ?: throw DecodeError("reed-solomon failed on block $bi")
            for (j in 0 until dataBlocks[bi].size) out.add(corrected[j])
        }
        return out.toIntArray()
    }

    private fun parseByteMode(data: IntArray, v: Int): String {
        val bits = ArrayList<Int>(data.size * 8)
        for (cw in data) for (i in 7 downTo 0) bits.add((cw shr i) and 1)
        var pos = 0
        // take reads k bits, but NEVER past the end. A malformed QR can claim a length or mode that
        // runs off the data; without this guard take() would throw IndexOutOfBounds on adversarial
        // input. We treat "not enough bits" as a decode error, not a crash.
        fun take(k: Int): Int {
            if (pos + k > bits.size) throw DecodeError("ran out of bits")
            var x = 0
            repeat(k) { x = (x shl 1) or bits[pos]; pos++ }
            return x
        }
        val mode = take(4)
        if (mode != 0b0100) throw DecodeError("not byte mode (got $mode)")
        val ccbits = if (v < 10) 8 else 16
        val len = take(ccbits)
        // Cap len to what the data could actually hold. A poisoned QR can put a huge length here; the
        // ByteArray(len) alloc and the read loop must be bounded by the real remaining bytes, or we
        // either over-read (caught above) or, with a very large len, risk a big allocation. The
        // remaining whole bytes is a hard ceiling.
        val maxBytes = (bits.size - pos) / 8
        if (len > maxBytes) throw DecodeError("length $len exceeds data ($maxBytes)")
        val bytes = ByteArray(len)
        for (i in 0 until len) bytes[i] = take(8).toByte()
        return String(bytes, Charsets.ISO_8859_1)
    }
}
