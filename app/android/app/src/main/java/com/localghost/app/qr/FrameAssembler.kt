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
    // Chunks bucketed by their frame's claimed identity (total, sha8), plus a vote count per identity.
    // A single garbled decode lands in its own bucket and is out-voted; it can never corrupt the real
    // set. The winning (most-voted) identity is the one assembled.
    private val chunksByIdent = HashMap<Pair<Int, String>, HashMap<Int, String>>()
    private val identVotes = HashMap<Pair<Int, String>, Int>()

    /** True if [text] is a multi-frame enrolment frame (and thus should be fed to [offer]). */
    fun isFrame(text: String): Boolean = text.startsWith("$MAGIC ")

    /** Frames captured so far and the total expected (0 total = nothing valid seen yet). */
    fun progress(): Pair<Int, Int> = (winningParts()?.size ?: 0) to total

    /** The set of frame indices (1-based) captured so far , drives per-frame checkmarks in the UI. */
    fun capturedSeqs(): Set<Int> = winningParts()?.keys?.toSet() ?: emptySet()

    // The chunk map for the currently most-voted identity, or null if nothing valid seen yet.
    private fun winningParts(): HashMap<Int, String>? {
        val best = identVotes.maxByOrNull { it.value }?.key ?: return null
        return chunksByIdent[best]
    }

    /** Whether the most recent [offer] added a frame the set did not already have. */
    var lastOfferWasNew: Boolean = false
        private set

    /** Discards accumulated frames , used when the user backs out or a mismatched set is detected. */
    fun reset() {
        total = 0
        sha8 = null
        chunksByIdent.clear()
        identVotes.clear()
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

        // Every frame in a set carries the SAME (total, sha8). A single garbled decode can present a
        // wrong total or sha8 with an otherwise-plausible header, and trusting the latest frame let one
        // bad read hijack the set (e.g. total 7 -> 6, then a phantom 6-set "completes" and fails to
        // parse). So we do not trust any single frame: (total, sha8) is decided by MAJORITY VOTE across
        // everything seen, and chunks are bucketed under their own frame's claimed identity. Only chunks
        // agreeing with the winning identity are assembled.
        val ident = tot to cksum
        identVotes[ident] = (identVotes[ident] ?: 0) + 1
        val bucket = chunksByIdent.getOrPut(ident) { HashMap() }
        // Always take the latest decode for a seq. A corrupt chunk that landed earlier is replaced when
        // the same frame is read cleanly on a later rotation , the whole-link checksum is the final
        // arbiter, so overwriting is safe and is how a bad chunk gets healed.
        val hadSeq = bucket.containsKey(seq)
        bucket[seq] = f[4]

        // Winning identity = most-voted (tot, sha8). Ties do not matter; more scans converge quickly.
        val best = identVotes.maxByOrNull { it.value }?.key ?: return null
        total = best.first
        sha8 = best.second
        val parts = chunksByIdent[best] ?: return null

        lastOfferWasNew = (ident == best) && !hadSeq

        if (parts.size < total) return null
        val sb = StringBuilder()
        for (i in 1..total) {
            parts[i]?.let { sb.append(it) } ?: return null
        }
        val link = sb.toString()
        if (sha8Of(link) == sha8) return link
        // Full set present but the join fails its own checksum: exactly one chunk is corrupt. Do NOT
        // clear , that would discard the good chunks too and loop. Keep them; the next clean decode of
        // whichever seq is bad overwrites it (see the always-take-latest above) and the following join
        // succeeds. Returning null keeps the UI in collecting mode meanwhile.
        return null
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
