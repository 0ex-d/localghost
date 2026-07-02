package com.localghost.app.net

import android.content.Context
import com.localghost.app.security.BoxConfig
import org.json.JSONObject
import java.io.BufferedReader
import java.net.HttpURLConnection
import java.net.URL
import javax.net.ssl.HttpsURLConnection

/**
 * The real HTTP transport to ghost.secd, over the pinned mTLS channel. No third-party HTTP library
 * (consistent with the app's zero-dependency stance): plain HttpsURLConnection with the socket
 * factory from BoxTrust (pins the box self-signed cert by fingerprint) presenting the device cert
 * from DeviceCert. org.json is built into Android, so no JSON dependency either.
 *
 * Every call needs the box config (baseUrl + pinned fingerprint) and the enrolled device identity.
 * If the device is not enrolled, calls fail fast , the app should be on the setup screen then.
 */
object BoxHttp {

    class NotEnrolled : Exception("device is not enrolled")

    private fun open(ctx: Context, path: String, method: String): HttpsURLConnection {
        val cfg = BoxConfig.read(ctx) ?: throw NotEnrolled()
        val km = DeviceCert.keyManager(ctx) ?: throw NotEnrolled()
        val conn = URL(cfg.baseUrl.trimEnd('/') + path).openConnection() as HttpsURLConnection
        conn.sslSocketFactory = BoxTrust.socketFactory(cfg.certFingerprint, km)
        // The fingerprint pin is the ENTIRE trust decision (BoxTrust): hostname/SAN checking against a
        // self-signed pinned cert adds nothing , no CA exists to mis-issue for a different host , and it
        // actively breaks the moment the box's LAN address changes (DHCP hands it a new IP, or the user
        // types a hostname where the cert has an IP). Without this, every call would die on hostname
        // verification with a perfectly matching pin.
        conn.hostnameVerifier = javax.net.ssl.HostnameVerifier { _, _ -> true }
        conn.requestMethod = method
        conn.connectTimeout = 10_000
        conn.readTimeout = 30_000
        if (cfg.deviceToken.isNotEmpty()) {
            conn.setRequestProperty("Authorization", "Bearer ${cfg.deviceToken}")
        }
        return conn
    }

    /** GET returning the parsed JSON object. */
    fun getJson(ctx: Context, path: String): JSONObject {
        val conn = open(ctx, path, "GET")
        return readJson(conn)
    }

    /** POST a JSON body, returning the parsed JSON response. */
    fun postJson(ctx: Context, path: String, body: JSONObject): JSONObject {
        val conn = open(ctx, path, "POST")
        conn.doOutput = true
        conn.setRequestProperty("Content-Type", "application/json")
        conn.outputStream.use { it.write(body.toString().toByteArray()) }
        return readJson(conn)
    }

    /**
     * Open a raw byte stream for a GET, with an optional Range start offset so a model download can
     * resume. The caller owns the stream and must close it. The underlying connection is closed when
     * the stream is exhausted/closed by the caller's use{} block.
     */
    fun openStream(ctx: Context, path: String, offset: Long): java.io.InputStream {
        val conn = open(ctx, path, "GET")
        if (offset > 0) {
            conn.setRequestProperty("Range", "bytes=$offset-")
        }
        val code = conn.responseCode
        if (code !in 200..299) {
            conn.disconnect()
            throw java.io.IOException("box returned HTTP $code for $path")
        }
        return conn.inputStream
    }

    /**
     * Enroll uses the pinned channel but WITHOUT a device cert yet (the device is being issued one).
     * It pins the box server cert by the fingerprint from the QR and posts the pairing code. This is
     * the one call that runs before DeviceCert exists.
     */
    fun enroll(baseUrl: String, pinnedFingerprint: String, body: JSONObject): JSONObject {
        val conn = URL(baseUrl.trimEnd('/') + "/v1/enroll").openConnection() as HttpsURLConnection
        // No client key manager yet; pin the server only. nginx allows /v1/enroll without a client
        // cert (it is the bootstrap endpoint); all other routes require the device cert.
        conn.sslSocketFactory = BoxTrust.socketFactory(pinnedFingerprint, EmptyKeyManager)
        // Same as open(): the QR fingerprint pin is the whole trust decision; hostname/SAN checking is
        // meaningless for a pinned self-signed cert and breaks enrolment when the QR carries an address
        // the cert's SAN doesn't happen to list.
        conn.hostnameVerifier = javax.net.ssl.HostnameVerifier { _, _ -> true }
        conn.requestMethod = "POST"
        conn.doOutput = true
        conn.connectTimeout = 10_000
        conn.readTimeout = 30_000
        conn.setRequestProperty("Content-Type", "application/json")
        conn.outputStream.use { it.write(body.toString().toByteArray()) }
        return readJson(conn)
    }

    private fun readJson(conn: HttpsURLConnection): JSONObject {
        return try {
            val code = conn.responseCode
            val stream = if (code in 200..299) conn.inputStream else conn.errorStream
            val text = stream?.bufferedReader()?.use(BufferedReader::readText) ?: "{}"
            JSONObject(text)
        } finally {
            conn.disconnect()
        }
    }
}
