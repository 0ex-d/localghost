package pair

import (
	"fmt"
	"net/url"
	"strings"
)

// EnrollLink is the box-side counterpart to the app's EnrollLink. The string it produces is what
// the QR carries and what EnrollLink.parse on the phone consumes, so the format is a contract:
//
//	localghost://enroll?v=1&host=...&port=...&code=...&fp=...&name=...
//
// host, code and fp are mandatory. Without the fingerprint the phone refuses the link, since the
// fingerprint is the trust anchor (no server vouches for the box).
//
// v is the format version. It MUST match the app's EnrollLink.CURRENT_VERSION. The app treats an
// absent v as 1 (older boxes shipped without it), so emitting it is backward-compatible, and it lets
// a newer box tell an older app to update rather than mis-parsing. Bump CurrentVersion here in lockstep
// with the app whenever the link format changes.
type EnrollLink struct {
	Host        string
	Port        int
	Code        string
	Fingerprint string
	BoxName     string
}

// CurrentVersion is the enrol-link format version this box emits. Keep equal to the app's
// EnrollLink.CURRENT_VERSION.
const CurrentVersion = 1

func (e EnrollLink) String() string {
	q := url.Values{}
	q.Set("v", fmt.Sprintf("%d", CurrentVersion))
	q.Set("host", e.Host)
	q.Set("port", fmt.Sprintf("%d", e.Port))
	q.Set("code", e.Code)
	// Fingerprint without separators keeps the QR payload short; the app re-inserts colons via
	// its normaliseFp. url.Values will percent-encode anything unusual, so plain hex is best.
	q.Set("fp", stripSeparators(e.Fingerprint))
	if e.BoxName != "" {
		q.Set("name", e.BoxName)
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
