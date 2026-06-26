package pair

import (
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
	code := strings.TrimSpace(q.Get("code"))
	fp := strings.TrimSpace(q.Get("fp"))
	if host == "" || code == "" || fp == "" {
		return EnrollLink{}, fmt.Errorf("link missing host, code, or fingerprint")
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
		Host:        host,
		Port:        port,
		Code:        code,
		Fingerprint: normaliseFp(fp),
		BoxName:     strings.TrimSpace(q.Get("name")),
	}, nil
}
