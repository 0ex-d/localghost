package pair

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Parse is the inverse of EnrollLink.String, mirroring the app's EnrollLink.parse. ghostctl uses
// it to connect from a link (typed, or read from the QR's payload), so you can enroll without
// standing at the box. host, code and fp are mandatory; the fingerprint is the trust anchor.
func Parse(raw string) (EnrollLink, error) {
	text := strings.TrimSpace(raw)
	const prefix = "localghost://enroll?"
	if !strings.HasPrefix(strings.ToLower(text), prefix) {
		return EnrollLink{}, fmt.Errorf("not a localghost enroll link")
	}
	q, err := url.ParseQuery(text[len(prefix):])
	if err != nil {
		return EnrollLink{}, err
	}
	host := strings.TrimSpace(q.Get("host"))
	fp := strings.TrimSpace(q.Get("fp"))
	if host == "" || fp == "" {
		return EnrollLink{}, fmt.Errorf("link missing host or fingerprint")
	}
	port := 8443
	if p := strings.TrimSpace(q.Get("port")); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return EnrollLink{}, fmt.Errorf("bad port %q", p)
		}
		port = n
	}
	return EnrollLink{
		Host:          host,
		Port:          port,
		Fingerprint:   normaliseFp(fp),
		BoxName:       strings.TrimSpace(q.Get("name")),
		DeviceCertDER: decodeB64URL(q.Get("cert")),
		DeviceKeyDER:  decodeB64URL(q.Get("key")),
	}, nil
}

// decodeB64URL decodes an unpadded-or-padded base64url value to its raw bytes (DER is binary, so
// no UTF-8 step), mirroring the app: empty or malformed input reads as absent rather than failing
// the whole link, because cert/key are optional at parse time (the enrol flow requires them).
func decodeB64URL(v string) []byte {
	v = strings.TrimRight(strings.TrimSpace(v), "=")
	if v == "" {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return nil
	}
	return b
}
