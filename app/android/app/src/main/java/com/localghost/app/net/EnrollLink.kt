package com.localghost.app.net

import java.net.URLDecoder

/**
 * The self-contained enrollment descriptor a box puts in its QR. No server is involved in
 * discovery or trust: the QR carries everything the phone needs to reach the box on the LAN and
 * to pin its identity on first contact.
 *
 *   localghost://enroll?v=1&host=192.168.1.20&port=8443&fp=AB:CD:...&name=xyntai&cert=<b64url>&key=<b64url>
 *
 * - v     : format version. Absent means 1 (a hand-typed link may omit it). The phone understands up
 *           to CURRENT_VERSION; a higher number means the box is newer than the app, and parse
 *           returns a Result.Outdated so the scanner can say "update the app". cert+key are part of
 *           v1: this is the first published format, nothing older exists in the wild.
 * - host  : LAN address or .local name the phone can route to right now.
 * - port  : the box's mTLS port.
 * - fp    : the box's certificate SHA-256 fingerprint. The trust anchor the phone pins on first
 *           contact, so enrollment is safe even over a hostile network. This fingerprint IS the vouch.
 * - name  : optional human label for the box (prefills the suggested device/box name).
 * - cert  : the DEVICE certificate (client cert) the box issued for this phone, raw DER, base64url-
 *           encoded. DER rather than PEM: PEM is base64 already, so base64url over PEM cost ~1.78x
 *           the DER and pushed a real identity past what the box's QR encoder can hold; DER-direct
 *           is ~1.33x and fits. DeviceCert consumed DER anyway (its PEM path stripped the armour
 *           before parsing), so nothing of value was lost.
 * - key   : that cert's PKCS8 PRIVATE KEY, raw DER, base64url-encoded. The box generates the keypair
 *           (the phone does not) and delivers both here, screen-to-camera , so enrollment is one scan
 *           with NO network call. The box never writes the key to its disk and zeroes its in-memory
 *           copy after rendering the QR; the phone imports these into its encrypted store
 *           (DeviceCert) and presents the cert on every later call. base64url (RFC 4648, URL-safe
 *           alphabet) so the binary survives the query string untouched.
 *
 * Remote access (away from home) is out of scope for the QR: the user types their own DDNS or VPN
 * host into the setup screen. The QR is the LAN/first-contact path.
 *
 * Parsing is pure (no android.net.Uri) so it can be unit-tested. cert/key are OPTIONAL at parse time
 * (a hand-typed link lacks them) but REQUIRED to actually enrol , the enrol flow rejects a link without
 * them, so a code-only link cannot silently half-enrol.
 */
data class EnrollLink(
    val host: String,
    val port: Int,
    val certFingerprint: String,
    val boxName: String = "",
    val version: Int = CURRENT_VERSION,
    // The box-issued device cert + its private key, raw DER, delivered in the QR. Null when the
    // link carried neither (hand-typed path); the enrol flow requires both. Note: ByteArray fields
    // make the data-class equals/hashCode compare these by REFERENCE; nothing compares EnrollLinks
    // for equality today, and content comparison belongs to tests (assertArrayEquals).
    val deviceCertDer: ByteArray? = null,
    val deviceKeyDer: ByteArray? = null,
) {
    /** The base URL the phone will store and connect to. */
    fun baseUrl(): String = "https://$host:$port"

    /** The outcome of parsing a scanned/typed payload. */
    sealed interface Result {
        data class Ok(val link: EnrollLink) : Result
        /** Looked like an enrol link but was malformed (missing required field, bad port). */
        object Malformed : Result
        /** A valid enrol link from a NEWER box than this app understands. Tell the user to update. */
        data class Outdated(val sawVersion: Int) : Result
        /** Not an enrol link at all (wrong scheme). */
        object NotEnroll : Result
    }

    companion object {
        const val PREFIX = "localghost://enroll?"
        /** The newest enrol-link version this app understands. Bump when the format changes. v1 is
         *  the first published format and includes cert+key. */
        const val CURRENT_VERSION = 1

        /**
         * Parse a scanned/typed payload, keeping the version distinction. Use this where the caller
         * wants to tell "newer box, update the app" apart from "garbage".
         */
        fun parseResult(raw: String): Result {
            val text = raw.trim()
            val lower = text.lowercase()
            if (!lower.startsWith(PREFIX)) return Result.NotEnroll
            val params = parseQuery(text.substring(PREFIX.length))

            // version first: absent = 1 (original codes). A higher version than we know means the box
            // is ahead of the app, which is a clean "update" message rather than a parse failure.
            val version = params["v"]?.trim()?.toIntOrNull() ?: 1
            if (version > CURRENT_VERSION) return Result.Outdated(version)

            val host = params["host"]?.trim().orEmpty()
            val fp = params["fp"]?.trim().orEmpty()
            val port = params["port"]?.trim()?.toIntOrNull() ?: 8443
            val name = params["name"]?.trim().orEmpty()
            // The device cert + key, base64url-encoded DER. Optional at parse time (hand-typed links
            // lack them); the enrol flow requires them. Malformed base64 -> treated as absent, not a hard fail.
            val certDer = decodeB64Url(params["cert"]?.trim().orEmpty())
            val keyDer = decodeB64Url(params["key"]?.trim().orEmpty())

            if (host.isEmpty() || fp.isEmpty()) return Result.Malformed
            if (port !in 1..65535) return Result.Malformed

            return Result.Ok(
                EnrollLink(
                    host = host, port = port,
                    certFingerprint = normaliseFp(fp), boxName = name, version = version,
                    deviceCertDer = certDer, deviceKeyDer = keyDer,
                )
            )
        }

        /** Parse a scanned/typed payload. Returns null if it isn't a valid, current enroll link. */
        fun parse(raw: String): EnrollLink? =
            (parseResult(raw) as? Result.Ok)?.link

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

        /** Decode a base64url (RFC 4648, URL-safe, optional padding) value to its raw bytes , DER is
         *  binary, so NO UTF-8 step (that would corrupt it) , or null if empty/invalid. Pure JVM
         *  (java.util.Base64) so it stays unit-testable. */
        private fun decodeB64Url(v: String): ByteArray? {
            if (v.isEmpty()) return null
            return runCatching {
                java.util.Base64.getUrlDecoder().decode(v.trimEnd('='))
            }.getOrNull()
        }
    }
}
