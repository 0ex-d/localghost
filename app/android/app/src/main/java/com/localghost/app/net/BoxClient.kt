package com.localghost.app.net

import kotlinx.coroutines.delay

/**
 * Talks to ghost.secd over mTLS (DeviceIdentity's cert authenticates the channel).
 * The PIN is sent INSIDE that channel; the box derives the key, mounts a persona,
 * and returns a session. The phone learns nothing about persona/mode — by design.
 *
 * STUB: uniform delay only. No resolution happens on-device, ever.
 */
object BoxClient {
    data class Session(val ok: Boolean)

    suspend fun submitPin(@Suppress("UNUSED_PARAMETER") pin: String): Session {
        // The fixed-deadline wait is authoritative on the BOX. This client-side
        // delay is only so the stub feels real; remove once secd owns the timing.
        delay(1500)
        return Session(ok = true)   // box always "succeeds" — garbage mounts empty-but-plausible
    }
}
