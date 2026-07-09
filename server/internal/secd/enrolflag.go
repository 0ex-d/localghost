package secd

// Enrolment-complete signal, one file. During provisioning ghost-setup rotates the enrolment QR and
// needs to know when the phone finished assembling its identity and made first contact , so it can
// stop rotating and confirm success instead of the operator eyeballing the app's frame counter.
//
// The box cannot observe individual frame scans (pre-enrolment the phone has no cert, so nginx's mTLS
// edge rejects it , which is the appears-down design). But once ALL frames are assembled the phone
// HAS its identity, and its first real request carries a client cert nginx verifies and forwards as
// X-Client-Cert. That first verified request is the completion signal. secd touches a flag file in
// its state dir; ghost-setup's rotation loop polls for it. No new endpoint, no new surface , just an
// existing authenticated request leaving a mark on disk.

import (
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
)

const enrolFlagName = "enrolled.flag"

// enrolFlagPath is where the first-verified-device marker lives (unencrypted state dir).
func (s *Server) enrolFlagPath() string { return filepath.Join(s.cfg.StateDir, enrolFlagName) }

// noteVerifiedDevice records that at least one authenticated device has reached secd. Idempotent and
// best-effort: the flag is a provisioning convenience, never a security control, so a write error is
// swallowed rather than failing the request that triggered it.
func (s *Server) noteVerifiedDevice() {
	if s.enrolFlagged.Swap(true) {
		return // already flagged this process; skip the syscall
	}
	_ = os.WriteFile(s.enrolFlagPath(), []byte("1\n"), 0o644)
}

// verifiedClient reports whether nginx forwarded a verified client cert for this request. nginx sets
// X-Client-Cert only after ssl_client_verify == SUCCESS (see the site config), so its presence is the
// edge's own attestation , secd does not re-parse the cert here, it only needs to know one arrived.
func verifiedClient(r *http.Request) bool {
	return r.Header.Get("X-Client-Cert") != ""
}

// enrolFlagField is embedded in Server (see server.go) to make noteVerifiedDevice a one-time syscall.
type enrolFlagField struct {
	enrolFlagged atomic.Bool
}
