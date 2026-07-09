package com.localghost.app.qr

import java.security.MessageDigest

/**
 * Reassembles a multi-frame enrolment QR. A real device identity (P256 cert + key) is ~1.4 KB, too
 * much for one comfortably-scannable QR, so ghost-setup renders several small frames and the phone
 * scans them in sequence. This is the app-side mirror of the box's pair.JoinFrames , the wire format
 * is a contract, so this file and internal/pair/qrchunk.go must stay in lockstep.
 *
 * Frame wire format, one line, ASCII:
 *
 *     LGQR1 <seq> <total> <sha8> <chunk>
 *
 * A plain (non-framed) enrol link , small enough for one QR , never matches MAGIC and passes straight
 * through, so single-QR boxes and this multi-frame path share one scanner with no mode switch.
 *
 * Order-independent and duplicate-safe: scanning frames in any order works, and re-scanning a frame
 * is idempotent. progress() drives the "3 of 6 captured" UI.
 */
class FrameAssembler {

    private var total = 0
    private var sha8: String? = null
    private val parts = HashMap<Int, String>()

    /** True if [text] is a multi-frame enrolment frame (and thus should be fed to [offer]). */
    fun isFrame(text: String): Boolean = text.startsWith("$MAGIC ")

    /** Frames captured so far and the total expected (0 total = nothing valid seen yet). */
    fun progress(): Pair<Int, Int> = parts.size to total

    /** The set of frame indices (1-based) captured so far , drives per-frame checkmarks in the UI. */
    fun capturedSeqs(): Set<Int> = parts.keys.toSet()

    /** Whether the most recent [offer] added a frame the set did not already have. */
    var lastOfferWasNew: Boolean = false
        private set

    /** Discards accumulated frames , used when the user backs out or a mismatched set is detected. */
    fun reset() {
        total = 0
        sha8 = null
        parts.clear()
    }

    /**
     * Offers one decoded frame. Returns the fully reassembled link once every frame is present and the
     * checksum verifies; null while more frames are needed. A frame from a different enrolment set (or a
     * malformed one) resets the accumulator and returns null , the person just keeps scanning the right
     * set.
     */
    fun offer(text: String): String? {
        val f = text.split(" ", limit = 5)
        if (f.size != 5 || f[0] != MAGIC) return null
        val seq = f[1].toIntOrNull() ?: return null
        val tot = f[2].toIntOrNull() ?: return null
        val cksum = f[3]
        if (seq < 1 || tot < 1 || seq > tot) return null

        if (total == 0) {
            total = tot
            sha8 = cksum
        } else if (tot != total || cksum != sha8) {
            // A frame from a different box/session. Start over on the new set.
            reset()
            total = tot
            sha8 = cksum
        }
        val hadIt = parts.containsKey(seq)
        parts[seq] = f[4]
        lastOfferWasNew = !hadIt

        if (parts.size < total) return null
        val sb = StringBuilder()
        for (i in 1..total) {
            parts[i]?.let { sb.append(it) } ?: return null
        }
        val link = sb.toString()
        return if (sha8Of(link) == sha8) link else null
    }

    private fun sha8Of(s: String): String {
        val d = MessageDigest.getInstance("SHA-256").digest(s.toByteArray(Charsets.UTF_8))
        val hex = StringBuilder(16)
        for (i in 0 until 4) hex.append("%02x".format(d[i]))
        return hex.toString()
    }

    companion object {
        const val MAGIC = "LGQR1"
    }
}
