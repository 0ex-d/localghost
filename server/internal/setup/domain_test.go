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
	// Verification stays OPTIONAL at the TLS layer for appears-down, not for any bootstrap: a
	// handshake reject is a tell, so a certless client completes the handshake and the decision
	// collapses to the uniform 503 inside.
	if !contains(out, "ssl_verify_client      optional") {
		t.Fatal("client cert verification must be optional , a handshake reject is a tell")
	}
	// There is NO enrolment endpoint anywhere: the QR carries the device identity, so no certless
	// location may exist. This assertion is the tripwire against one creeping back in.
	if contains(out, "/v1/enroll") {
		t.Fatal("no enrolment location may exist , enrolment is the QR scan itself")
	}
	// Every route must reject an unverified client cert inside, with the uniform 503.
	if !contains(out, "$ssl_client_verify != SUCCESS") {
		t.Fatal("all routes must require a verified device cert")
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
