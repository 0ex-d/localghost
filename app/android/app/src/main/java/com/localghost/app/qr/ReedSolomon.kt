package com.localghost.app.qr

/**
 * Reed-Solomon errors-only decoder over GF(256) for QR. Ported faithfully from the reference
 * implementation that was validated against round-trip tests (1..t errors corrected, boundary
 * positions, and beyond-capacity correctly rejected rather than mis-corrected).
 *
 * decode(block, nsym) returns the corrected block, or null if it cannot be corrected (too many
 * errors). nsym is the number of EC codewords in the block. Polynomials are high-order-first,
 * matching the reference's list conventions.
 *
 * A null return is the safe outcome: in the trust-anchor path we would rather fail to scan than
 * "correct" to wrong bytes.
 */
internal object ReedSolomon {
    private val gf = GaloisField

    fun decode(block: IntArray, nsym: Int): IntArray? {
        val synd = calcSyndromes(block, nsym)
        if (synd.max() == 0) return block          // no errors

        val errLoc = findErrorLocator(synd, nsym)
        val positions = findErrors(errLoc.reversedArray(), block.size) ?: return null
        return correctErrata(block, synd, positions)
    }

    // synd has length nsym+1 with a leading 0, matching the reference.
    private fun calcSyndromes(msg: IntArray, nsym: Int): IntArray {
        val out = IntArray(nsym + 1)
        for (i in 0 until nsym) out[i + 1] = polyEval(msg, gf.pow(2, i))
        return out
    }

    private fun polyEval(p: IntArray, x: Int): Int {
        var y = p[0]
        for (i in 1 until p.size) y = gf.mul(y, x) xor p[i]
        return y
    }

    private fun polyScale(p: IntArray, x: Int): IntArray =
        IntArray(p.size) { gf.mul(p[it], x) }

    private fun polyAdd(p: IntArray, q: IntArray): IntArray {
        val r = IntArray(maxOf(p.size, q.size))
        for (i in p.indices) r[i + r.size - p.size] = p[i]
        for (i in q.indices) r[i + r.size - q.size] = r[i + r.size - q.size] xor q[i]
        return r
    }

    private fun polyMul(p: IntArray, q: IntArray): IntArray {
        val r = IntArray(p.size + q.size - 1)
        for (j in q.indices) for (i in p.indices) r[i + j] = r[i + j] xor gf.mul(p[i], q[j])
        return r
    }

    private fun findErrorLocator(synd: IntArray, nsym: Int): IntArray {
        var errLoc = intArrayOf(1)
        var oldLoc = intArrayOf(1)
        val syndShift = if (synd.size > nsym) synd.size - nsym else 0
        for (i in 0 until nsym) {
            val k = i + syndShift
            var delta = synd[k]
            for (j in 1 until errLoc.size) {
                delta = delta xor gf.mul(errLoc[errLoc.size - 1 - j], synd[k - j])
            }
            oldLoc = oldLoc + 0
            if (delta != 0) {
                if (oldLoc.size > errLoc.size) {
                    val newLoc = polyScale(oldLoc, delta)
                    oldLoc = polyScale(errLoc, gf.inverse(delta))
                    errLoc = newLoc
                }
                errLoc = polyAdd(errLoc, polyScale(oldLoc, delta))
            }
        }
        var start = 0
        while (start < errLoc.size && errLoc[start] == 0) start++
        return errLoc.copyOfRange(start, errLoc.size)
    }

    // err_loc passed in already reversed (low-order first), matching reference call site.
    private fun findErrors(errLocRev: IntArray, nmess: Int): IntArray? {
        val errs = errLocRev.size - 1
        val positions = ArrayList<Int>()
        for (i in 0 until nmess) {
            if (polyEval(errLocRev, gf.pow(2, i)) == 0) positions.add(nmess - 1 - i)
        }
        if (positions.size != errs) return null      // too many errors to locate
        return positions.toIntArray()
    }

    private fun findErrataLocator(ePos: IntArray): IntArray {
        var eLoc = intArrayOf(1)
        for (i in ePos) {
            eLoc = polyMul(eLoc, polyAdd(intArrayOf(1), intArrayOf(gf.pow(2, i), 0)))
        }
        return eLoc
    }

    private fun findErrorEvaluator(synd: IntArray, errLoc: IntArray, nsym: Int): IntArray {
        val remainder = polyMul(synd, errLoc)
        return remainder.copyOfRange(remainder.size - (nsym + 1), remainder.size)
    }

    private fun correctErrata(msgIn: IntArray, synd: IntArray, errPos: IntArray): IntArray? {
        val coefPos = IntArray(errPos.size) { msgIn.size - 1 - errPos[it] }
        val errLoc = findErrataLocator(coefPos)
        val errEval = findErrorEvaluator(synd.reversedArray(), errLoc, errLoc.size - 1).reversedArray()

        val x = IntArray(coefPos.size) { gf.pow(2, coefPos[it]) }
        val e = IntArray(msgIn.size)
        for (i in x.indices) {
            val xiInv = gf.inverse(x[i])
            var errLocPrime = 1
            for (j in x.indices) {
                if (j != i) errLocPrime = gf.mul(errLocPrime, 1 xor gf.mul(xiInv, x[j]))
            }
            if (errLocPrime == 0) return null
            var y = polyEval(errEval.reversedArray(), xiInv)
            y = gf.mul(gf.pow(x[i], 1), y)
            e[errPos[i]] = gf.div(y, errLocPrime)
        }
        return polyAdd(msgIn, e)
    }
}
