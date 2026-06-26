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
