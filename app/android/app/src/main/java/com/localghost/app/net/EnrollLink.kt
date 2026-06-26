package com.localghost.app.net

import java.net.URLDecoder

/**
 * The self-contained enrollment descriptor a box puts in its QR. No server is involved in
 * discovery or trust: the QR carries everything the phone needs to reach the box on the LAN and
 * to pin its identity on first contact.
 *
 *   localghost://enroll?host=192.168.1.20&port=8443&code=ABCD-1234&fp=AB:CD:...&name=xyntai
 *
 * - host  : LAN address or .local name the phone can route to right now.
 * - port  : the box's mTLS port.
 * - code  : one-time pairing code the box is showing.
 * - fp    : the box's certificate SHA-256 fingerprint. This is the trust anchor. The phone pins
 *           it on the first connection, so enrollment is safe even over a hostile network. Without
 *           a server vouching for the box, this fingerprint IS the vouch.
 * - name  : optional human label for the box (prefills the suggested device/box name).
 *
 * Remote access (away from home) is out of scope for the QR: the user types their own DDNS or VPN
 * host into the setup screen. The QR is the LAN/first-contact path.
 *
 * Parsing is pure (no android.net.Uri) so it can be unit-tested. It is the trust anchor, so it
 * refuses anything missing host, code, or fingerprint rather than enrolling insecurely.
 */
data class EnrollLink(
    val host: String,
    val port: Int,
    val code: String,
    val certFingerprint: String,
    val boxName: String = "",
) {
    /** The base URL the phone will store and connect to. */
    fun baseUrl(): String = "https://$host:$port"

    companion object {
        const val PREFIX = "localghost://enroll?"

        /** Parse a scanned/typed payload. Returns null if it isn't a valid enroll link. */
        fun parse(raw: String): EnrollLink? {
            val text = raw.trim()
            // Case-insensitive scheme+host check.
            val lower = text.lowercase()
            if (!lower.startsWith(PREFIX)) return null
            val query = text.substring(PREFIX.length)

            val params = parseQuery(query)
            val host = params["host"]?.trim().orEmpty()
            val code = params["code"]?.trim().orEmpty()
            val fp = params["fp"]?.trim().orEmpty()
            val port = params["port"]?.trim()?.toIntOrNull() ?: 8443
            val name = params["name"]?.trim().orEmpty()

            // host, code, and fingerprint are all required: without the fingerprint there is no
            // trust anchor, so we refuse rather than enroll insecurely.
            if (host.isEmpty() || code.isEmpty() || fp.isEmpty()) return null
            if (port !in 1..65535) return null

            return EnrollLink(host = host, port = port, code = code,
                certFingerprint = normaliseFp(fp), boxName = name)
        }

        private fun parseQuery(query: String): Map<String, String> =
            query.split("&").mapNotNull { pair ->
                val i = pair.indexOf('=')
                if (i <= 0) return@mapNotNull null
                val k = pair.substring(0, i)
                val v = pair.substring(i + 1)
                runCatching {
                    URLDecoder.decode(k, "UTF-8") to URLDecoder.decode(v, "UTF-8")
                }.getOrNull()
            }.toMap()

        /** Normalise a fingerprint to uppercase colon-separated hex for stable comparison. */
        fun normaliseFp(fp: String): String =
            fp.uppercase().filter { it.isLetterOrDigit() }
                .chunked(2).joinToString(":")
    }
}
