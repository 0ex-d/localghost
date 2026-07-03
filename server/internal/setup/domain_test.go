package setup

import (
	"net"
	"testing"
)

func cfg() DomainConfig {
	return DomainConfig{Domain: "vlad.localghost.ai", PublicIPv4: "203.0.113.7"}
}

func TestDNSMatches(t *testing.T) {
	err := cfg().VerifyDNS(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.7")}, nil
	})
	if err != nil {
		t.Fatalf("correct A record should verify: %v", err)
	}
}

func TestDNSMismatch(t *testing.T) {
	err := cfg().VerifyDNS(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("198.51.100.9")}, nil
	})
	if err == nil {
		t.Fatal("a record pointing elsewhere must fail")
	}
}

func TestDNSUnresolved(t *testing.T) {
	err := cfg().VerifyDNS(func(string) ([]net.IP, error) { return nil, nil })
	if err == nil {
		t.Fatal("an unresolved domain must fail")
	}
}

func TestNoDomain(t *testing.T) {
	c := DomainConfig{Domain: "", PublicIPv4: "203.0.113.7"}
	if err := c.VerifyDNS(func(string) ([]net.IP, error) { return nil, nil }); err != ErrNoDomain {
		t.Fatalf("empty domain must be ErrNoDomain, got %v", err)
	}
}

func TestNginxMentionsDomainAndGhostSecd(t *testing.T) {
	out := cfg().NginxConfig("127.0.0.1:8443")
	if !contains(out, "vlad.localghost.ai") || !contains(out, "127.0.0.1:8443") {
		t.Fatal("nginx config must reference the domain and the ghost.secd address")
	}
	// ssl_verify_client is OPTIONAL, not on: a hard "on" makes nginx reject the TLS handshake for a
	// certless client, which is itself a tell (a down server accepts the connection and says nothing).
	// Optional + a uniform 503 for unverified clients is what makes the box appear down, not refusing.
	if !contains(out, "ssl_verify_client      optional") {
		t.Fatal("client cert verification must be OPTIONAL (never on) for the appears-down model")
	}
	// Enrolment was removed: the QR carries the device cert directly, so there is NO /v1/enroll
	// location and NO certless bootstrap path. Assert it is absent , a reintroduced enrol endpoint
	// would be a certless hole in the edge.
	if contains(out, "/v1/enroll") {
		t.Fatal("there must be no /v1/enroll location , the QR delivers the cert, scanning is enrolment")
	}
	// Every other route must reject an unverified client cert at the edge.
	if !contains(out, "$ssl_client_verify != SUCCESS") {
		t.Fatal("non-enrol routes must require a verified device cert")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
