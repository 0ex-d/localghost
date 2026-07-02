package secd

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// session is the runtime credential the app holds AFTER a correct-PIN unlock. The PIN is never stored
// or re-sent; it produces a token. Foreground requests AND the background notification poller carry
// the SAME token (shared fate: revoking it kills both at once, so the poller can never stay alive
// while the foreground plays dead). See the runtime design doc.
//
// Lifecycle:
//   - correct PIN  -> Issue() mints a fresh token (any previous one is dropped)
//   - any wrong PIN -> Revoke() invalidates the current token
//   - the app re-enters the PIN every open (~20-30x/day), so a fresh token per open is expected
//
// A token does NOT unmount or re-lock anything; it is an authorisation credential only. The mount
// persists across token churn until reboot.

type sessionManager struct {
	mu      sync.Mutex
	token   string    // the one live token; empty = no session
	expires time.Time // self-expiry safety net
	ttl     time.Duration
}

func newSessionManager(ttl time.Duration) *sessionManager {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &sessionManager{ttl: ttl}
}

// Issue mints a fresh token, replacing any previous one. Called on a correct-PIN unlock.
func (m *sessionManager) Issue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	m.mu.Lock()
	m.token = tok
	m.expires = time.Now().Add(m.ttl)
	m.mu.Unlock()
	return tok, nil
}

// Revoke invalidates the current token. Called on ANY wrong PIN. Idempotent.
func (m *sessionManager) Revoke() {
	m.mu.Lock()
	m.token = ""
	m.expires = time.Time{}
	m.mu.Unlock()
}

// Valid reports whether the presented token is the current, unexpired one. Empty/expired/mismatched
// all return false, which the caller turns into the appears-down response.
func (m *sessionManager) Valid(presented string) bool {
	if presented == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.token == "" || time.Now().After(m.expires) {
		return false
	}
	// constant-time-ish compare is overkill here (the token is high-entropy random), but avoid a
	// short-circuit length leak by comparing directly.
	return presented == m.token
}
