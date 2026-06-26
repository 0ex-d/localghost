package com.localghost.app.qr

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.random.Random

/**
 * Guards the RS decoder. The algorithm was validated in a reference harness (round-trip over many
 * random corruptions); these tests pin the same behaviour in the shipped Kotlin so a future edit
 * can't silently break error correction in the QR trust-anchor path.
 *
 * A known message + its RS parity (nsym=10) is built here with a small encoder, then corrupted and
 * decoded.
 */
class ReedSolomonTest {

    private val gf = GaloisField

    // Minimal RS encoder (generator 2), only for producing test vectors.
    private fun genPoly(nsym: Int): IntArray {
        var g = intArrayOf(1)
        for (i in 0 until nsym) g = polyMul(g, intArrayOf(1, gf.pow(2, i)))
        return g
    }
    private fun polyMul(p: IntArray, q: IntArray): IntArray {
        val r = IntArray(p.size + q.size - 1)
        for (j in q.indices) for (i in p.indices) r[i + j] = r[i + j] xor gf.mul(p[i], q[j])
        return r
    }
    private fun encode(msg: IntArray, nsym: Int): IntArray {
        val g = genPoly(nsym)
        val out = IntArray(msg.size + nsym)
        msg.copyInto(out)
        for (i in msg.indices) {
            val c = out[i]
            if (c != 0) for (j in 1 until g.size) out[i + j] = out[i + j] xor gf.mul(g[j], c)
        }
        msg.copyInto(out)
        return out
    }

    private val msg = intArrayOf(
        0x40, 0xD2, 0x75, 0x47, 0x76, 0x17, 0x32, 0x06,
        0x27, 0x26, 0x96, 0xC6, 0xC6, 0x96, 0x70, 0xEC)
    private val nsym = 10
    private val code = encode(msg, nsym)

    @Test fun cleanBlockUnchanged() {
        assertArrayEquals(code, ReedSolomon.decode(code.copyOf(), nsym))
    }

    @Test fun singleError() {
        val c = code.copyOf(); c[7] = c[7] xor 0x9A
        assertArrayEquals(code, ReedSolomon.decode(c, nsym))
    }

    @Test fun twoErrors() {
        val c = code.copyOf(); c[0] = c[0] xor 0xFF; c[12] = c[12] xor 0x01
        assertArrayEquals(code, ReedSolomon.decode(c, nsym))
    }

    @Test fun maxCorrectableErrors() {
        val c = code.copyOf()
        intArrayOf(0, 3, 6, 9, 12).forEachIndexed { i, p -> c[p] = c[p] xor (i + 1) }
        assertArrayEquals(code, ReedSolomon.decode(c, nsym))
    }

    @Test fun boundaryErrors() {
        val c = code.copyOf(); c[0] = c[0] xor 0x77; c[code.size - 1] = c[code.size - 1] xor 0x55
        assertArrayEquals(code, ReedSolomon.decode(c, nsym))
    }

    @Test fun beyondCapacityReturnsNull() {
        val c = code.copyOf()
        intArrayOf(0, 2, 4, 6, 8, 10).forEach { c[it] = c[it] xor 0x5A }   // 6 > t=5
        assertNull("must not mis-correct beyond capacity", ReedSolomon.decode(c, nsym))
    }

    @Test fun manyRandomCorruptions() {
        val rnd = Random(1)
        repeat(200) {
            val nerr = rnd.nextInt(0, 6)               // 0..5
            val c = code.copyOf()
            val positions = (code.indices).shuffled(rnd).take(nerr)
            positions.forEach { c[it] = c[it] xor rnd.nextInt(1, 256) }
            assertArrayEquals("nerr=$nerr pos=$positions", code, ReedSolomon.decode(c, nsym))
        }
    }
}
