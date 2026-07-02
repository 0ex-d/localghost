package integration

import "errors"

// An integration is a per-account connector: a bank link, a calendar, an email account. Its config
// and secrets (OAuth tokens, refresh tokens, API keys) are part of THAT account's encrypted store,
// so they decrypt only when the account is mounted and are sealed under the account's own key. They
// are never global. The isolation is the financial-data boundary, enforced by where the bytes live,
// not by a policy to remember.
//
// State controls live behaviour. An ENABLED integration means a live token that refreshes and a
// daemon polling on a schedule. New integrations default to PAUSED (present and configured, but not
// polling); you Enable them explicitly, so nothing starts live background work by accident.
type State int

const (
	Paused  State = iota // configured but not polling; no token refresh, no background activity
	Enabled              // live: token refreshes, daemons poll
)

func (s State) String() string {
	if s == Enabled {
		return "enabled"
	}
	return "paused"
}

// Kind is the connector type. Extend as connectors are added.
type Kind string

const (
	Bank     Kind = "bank"
	Calendar Kind = "calendar"
	Email    Kind = "email"
	Cloud    Kind = "cloud"
)

// Integration is one connector within an account. Secret holds the sensitive material (tokens); it
// is only ever in memory while the account is mounted, and is persisted as part of the account's
// encrypted store, never separately.
type Integration struct {
	ID     string
	Kind   Kind
	Label  string // human label, e.g. "Monzo"
	State  State
	Secret []byte // OAuth/refresh tokens etc.; sealed with the account, zeroised on unmount
}

// Polls reports whether this integration should be doing live background work. Only Enabled
// integrations poll; a Paused one sits silent.
func (i Integration) Polls() bool { return i.State == Enabled }

var (
	ErrNotFound          = errors.New("integration not found")
	ErrExists            = errors.New("integration already exists")
)
