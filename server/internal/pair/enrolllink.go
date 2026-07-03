package pair

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

// EnrollLink is the box-side counterpart to the app's EnrollLink. The string it produces is what
// the QR carries and what EnrollLink.parse on the phone consumes, so the format is a contract:
//
//	localghost://enroll?v=1&host=...&port=...&fp=...&name=...&cert=<b64url>&key=<b64url>
//
// host and fp are mandatory. Without the fingerprint the phone refuses the link, since the
// fingerprint is the trust anchor (no server vouches for the box). There is no pairing code: the
// old code-for-cert exchange is gone (the QR carries the cert), and a code nothing redeems would be
// security theater , a secret in every link that no one checks.
//
// cert and key carry the box-issued DEVICE certificate and its private key: raw DER, base64url-
// encoded (RFC 4648 URL-safe alphabet, no padding, so url.Values never percent-encodes them). DER,
// not PEM: PEM is itself base64, so base64url over PEM costs ~1.78x the DER while DER-direct costs
// ~1.33x , the difference between a link that fits the QR encoder and one that does not. The app
// feeds DER straight to CertificateFactory/PKCS8EncodedKeySpec anyway (its PEM path stripped the
// armour first), so PEM in the link was pure inflation. The box generates the keypair, the phone
// does not; delivery is screen-to-camera, so enrolment is one scan with no network call, the key is
// never written to the box's disk, and the in-memory copy is zeroed after the QR is rendered. They
// are optional in the STRING (a hand-typed link omits them) but the app's enrol flow requires both,
// so a link without them cannot silently half-enrol.
//
// Size, with real numbers: P-256 device cert ~380-500 bytes DER (~510-670 b64url) + PKCS8 key 138
// DER (184) + base link ~130. Total ~820-980. Above the encoder's 666-byte level-M ceiling, inside
// the 858-byte level-L fallback for certs the box actually mints (it controls the subject and
// extensions, so it keeps them minimal). A code-only link stays tiny and stays level M.
//
// v is the format version. It MUST match the app's EnrollLink.CURRENT_VERSION. The app treats an
// absent v as 1 (a hand-typed link may omit it), so emitting it is backward-compatible, and it lets
// a newer box tell an older app to update rather than mis-parsing. cert+key are part of v1: this is
// the first published format, so there is no earlier code-only version in the wild. Bump
// CurrentVersion here in lockstep with the app whenever the link format changes.
type EnrollLink struct {
	Host        string
	Port        int
	Fingerprint string
	BoxName     string
	// DeviceCertDER and DeviceKeyDER are the box-issued client certificate and its PKCS8 private
	// key, raw DER bytes. Nil/empty means the link carries no credential (hand-typed path).
	DeviceCertDER []byte
	DeviceKeyDER  []byte
}

// CurrentVersion is the enrol-link format version this box emits. Keep equal to the app's
// EnrollLink.CURRENT_VERSION.
const CurrentVersion = 1

func (e EnrollLink) String() string {
	q := url.Values{}
	q.Set("v", fmt.Sprintf("%d", CurrentVersion))
	q.Set("host", e.Host)
	q.Set("port", fmt.Sprintf("%d", e.Port))
	// Fingerprint without separators keeps the QR payload short; the app re-inserts colons via
	// its normaliseFp. url.Values will percent-encode anything unusual, so plain hex is best.
	q.Set("fp", stripSeparators(e.Fingerprint))
	if e.BoxName != "" {
		q.Set("name", e.BoxName)
	}
	// base64url without padding: the URL-safe alphabet passes through url.Values.Encode untouched
	// (no percent-encoding, which would inflate the QR), and the app's decoder strips padding anyway.
	if len(e.DeviceCertDER) > 0 {
		q.Set("cert", base64.RawURLEncoding.EncodeToString(e.DeviceCertDER))
	}
	if len(e.DeviceKeyDER) > 0 {
		q.Set("key", base64.RawURLEncoding.EncodeToString(e.DeviceKeyDER))
	}
	// url.Values.Encode sorts keys; the app parser is order-independent, so that is fine.
	return "localghost://enroll?" + q.Encode()
}

// stripSeparators reduces a fingerprint to bare uppercase hex (no colons) for a compact QR.
func stripSeparators(fp string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(fp) {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normaliseFp matches the app: uppercase, colon-separated hex pairs.
func normaliseFp(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(raw) {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') {
			b.WriteRune(r)
		}
	}
	hexStr := b.String()
	var pairs []string
	for i := 0; i+1 < len(hexStr); i += 2 {
		pairs = append(pairs, hexStr[i:i+2])
	}
	return strings.Join(pairs, ":")
}
