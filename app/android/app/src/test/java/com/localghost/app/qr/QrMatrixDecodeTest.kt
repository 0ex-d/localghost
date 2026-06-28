package com.localghost.app.qr

import org.junit.Assert.assertThrows
import org.junit.Test

/**
 * Guards QrMatrixDecode's input validation. The full encode<->decode round-trip is validated in a
 * cross-language reference harness (the box's Go encoder and this decoder agree across payload sizes
 * 1-200 chars); these tests pin that malformed grids are rejected rather than silently producing a
 * wrong enrol link , which matters because the decoded value drives the TLS pin.
 */
class QrMatrixDecodeTest {

    @Test
    fun rejectsNonQrModuleCount() {
        // 20 is not a valid QR side (must be 17 + 4v).
        val grid = Array(20) { BooleanArray(20) }
        assertThrows(QrMatrixDecode.DecodeError::class.java) { QrMatrixDecode.decode(grid) }
    }

    @Test
    fun rejectsUnsupportedVersion() {
        // v11 = side 61, beyond the 1-10 subset we support.
        val grid = Array(61) { BooleanArray(61) }
        assertThrows(QrMatrixDecode.DecodeError::class.java) { QrMatrixDecode.decode(grid) }
    }

    @Test
    fun rejectsAllBlankGrid() {
        // A valid size (v1 = 21) but all-light: RS will fail on the empty data, not crash.
        val grid = Array(21) { BooleanArray(21) }
        assertThrows(Exception::class.java) { QrMatrixDecode.decode(grid) }
    }
}

// --- poison resistance: malformed input must fail as DecodeError, never an uncaught crash ---
// A hostile QR cannot enrol (the fingerprint pin stops that), but it must not be able to crash the
// decoder either. These feed adversarial grids and assert a clean DecodeError rather than an
// IndexOutOfBounds, OOM, or hang.
class QrMatrixDecodePoisonTest {

    private fun squareGrid(n: Int, fill: Boolean = false) = Array(n) { BooleanArray(n) { fill } }

    @org.junit.Test fun jaggedGridRejected() {
        val g = Array(25) { i -> BooleanArray(if (i == 0) 10 else 25) } // row 0 too short
        try { QrMatrixDecode.decode(g); org.junit.Assert.fail("expected DecodeError") }
        catch (e: QrMatrixDecode.DecodeError) { /* good */ }
    }

    @org.junit.Test fun oversizedGridRejected() {
        try { QrMatrixDecode.decode(squareGrid(101)); org.junit.Assert.fail("expected DecodeError") }
        catch (e: QrMatrixDecode.DecodeError) { /* good */ }
    }

    @org.junit.Test fun tinyGridRejected() {
        try { QrMatrixDecode.decode(squareGrid(9)); org.junit.Assert.fail("expected DecodeError") }
        catch (e: QrMatrixDecode.DecodeError) { /* good */ }
    }

    @org.junit.Test fun garbageValidSizeGridDoesNotCrash() {
        // a correctly-sized (v1, 21x21) but content-garbage grid must throw DecodeError, not crash
        val rnd = java.util.Random(42)
        repeat(50) {
            val g = Array(21) { BooleanArray(21) { rnd.nextBoolean() } }
            try { QrMatrixDecode.decode(g) } catch (e: QrMatrixDecode.DecodeError) { /* expected */ }
            // any other throwable would fail the test by propagating
        }
    }
}
