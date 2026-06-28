package com.localghost.app.qr

/**
 * Classifies a QR that decoded fine but is not a LocalGhost enrol link, and hands back something dry to
 * say about it. The point is to break the old silence on a readable-but-wrong code. The scanner used to
 * fail quietly, so a person pointing at, say, a WiFi code had no idea whether the scanner was working,
 * mis-aimed, or broken. Now it tells them what they actually scanned and has a small opinion about it.
 *
 * This only runs when the decode SUCCEEDED and the text is not an enrol link. A frame with no QR, or an
 * unreadable one, never reaches here, because there is nothing funny about someone struggling to land a
 * scan and a joke there would just be annoying.
 */
data class QrGuess(val label: String, val quip: String, val preview: String)

object QrContent {

    fun classify(text: String): QrGuess {
        val t = text.trim()
        val lower = t.lowercase()
        val preview = if (t.length > 60) t.take(57) + "..." else t

        return when {
            lower.startsWith("wifi:") -> QrGuess(
                "a WiFi network",
                "That is someone's WiFi. I keep secrets, I do not join other people's networks.",
                preview,
            )
            lower.startsWith("begin:vcard") -> QrGuess(
                "a contact card",
                "A contact card. Lovely. I am not a phonebook though.",
                preview,
            )
            lower.startsWith("bitcoin:") || lower.startsWith("ethereum:") || lower.startsWith("lightning:") -> QrGuess(
                "a crypto address",
                "Someone wants paying. Not by me, and not through here.",
                preview,
            )
            lower.startsWith("mailto:") -> QrGuess(
                "an email address",
                "That opens an email. I do not do email. The box might, if you ask it nicely.",
                preview,
            )
            lower.startsWith("tel:") || lower.startsWith("sms:") -> QrGuess(
                "a phone number",
                "A phone number. I am flattered, but I only call the box.",
                preview,
            )
            lower.startsWith("otpauth:") -> QrGuess(
                "a 2FA secret",
                "That is a two-factor seed. Put it somewhere safer than my camera.",
                preview,
            )
            lower.startsWith("http://") || lower.startsWith("https://") -> QrGuess(
                "a web link",
                "That is a link to the open web. I only speak box.",
                preview,
            )
            lower.startsWith("localghost://") -> QrGuess(
                "a LocalGhost link, but a malformed one",
                "Almost. That is one of mine, but it did not parse. Try a fresh one from setup.",
                preview,
            )
            t.toDoubleOrNull() != null || t.all { it.isDigit() } -> QrGuess(
                "a number",
                "Just a number. I have no idea what to do with it, and neither do you.",
                preview,
            )
            else -> QrGuess(
                "some text",
                "Readable, but not an enrol link. Whatever it is, it is not the way in.",
                preview,
            )
        }
    }
}
