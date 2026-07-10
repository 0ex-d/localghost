package com.localghost.app.sync

/**
 * Turns a stream of "bytes sent so far" ticks into a smoothed speed and an ETA. Kept deliberately
 * simple: an exponential moving average over the instantaneous rate between ticks, so a stalled photo
 * or a fast one does not whipsaw the estimate. Wall-clock based (System.nanoTime), so it measures
 * real upload throughput including the mTLS overhead , which is what the person actually waits on.
 *
 * Not thread-safe by design: it is driven from the single sync coroutine's progress callbacks.
 */
class SyncMeter(private val totalBytes: Long) {
    private var startedNanos = 0L
    private var lastNanos = 0L
    private var lastBytes = 0L
    private var emaBps = 0.0
    private val alpha = 0.3 // smoothing: higher = snappier, lower = steadier

    /** Feed the cumulative bytes sent this run. Returns (smoothedBytesPerSec, etaSeconds). */
    fun update(bytesSentTotal: Long): Pair<Double, Long> {
        val now = System.nanoTime()
        if (startedNanos == 0L) {
            startedNanos = now; lastNanos = now; lastBytes = bytesSentTotal
            return 0.0 to 0L
        }
        val dtSec = (now - lastNanos) / 1_000_000_000.0
        if (dtSec >= 0.25) { // only re-estimate a few times a second; sub-tick noise is meaningless
            val dBytes = bytesSentTotal - lastBytes
            if (dBytes >= 0 && dtSec > 0) {
                val inst = dBytes / dtSec
                emaBps = if (emaBps == 0.0) inst else alpha * inst + (1 - alpha) * emaBps
            }
            lastNanos = now; lastBytes = bytesSentTotal
        }
        val remaining = (totalBytes - bytesSentTotal).coerceAtLeast(0)
        val eta = if (emaBps > 1.0) (remaining / emaBps).toLong() else 0L
        return emaBps to eta
    }
}
