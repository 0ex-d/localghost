package com.localghost.app.qr

/**
 * GF(256) arithmetic for QR Reed-Solomon, primitive polynomial 0x11D, generator 2.
 * Pure, no Android. Ported from the validated reference (Wikiversity "Reed-Solomon codes for
 * coders"); the exp table is doubled to 512 so mul/pow need no modulo on lookups.
 */
internal object GaloisField {
    private const val PRIMITIVE = 0x11D
    val exp = IntArray(512)
    val log = IntArray(256)

    init {
        var x = 1
        for (i in 0 until 255) {
            exp[i] = x
            log[x] = i
            x = x shl 1
            if (x and 0x100 != 0) x = x xor PRIMITIVE
        }
        for (i in 255 until 512) exp[i] = exp[i - 255]
    }

    fun mul(a: Int, b: Int): Int =
        if (a == 0 || b == 0) 0 else exp[log[a] + log[b]]

    fun div(a: Int, b: Int): Int {
        require(b != 0) { "GF divide by zero" }
        return if (a == 0) 0 else exp[(log[a] + 255 - log[b]) % 255]
    }

    fun pow(x: Int, power: Int): Int = exp[(log[x] * power) % 255]

    fun inverse(a: Int): Int = exp[255 - log[a]]
}
