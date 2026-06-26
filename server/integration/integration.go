package integration

import "errors"

// An integration is a per-account connector: a bank link, a calendar, an email account. Its config
// and secrets (OAuth tokens, refresh tokens, API keys) are part of THAT account's encrypted store,
// so they decrypt only when the account is mounted and are sealed under the account's own key. They
// are never global. A decoy account literally cannot reach the main account's bank tokens, because
// they are wrapped under the main's AMK + PIN, which the decoy does not have. That isolation is the
// financial-data boundary, enforced by where the bytes live, not by a policy to remember.
//
// State matters for deniability. An ENABLED integration usually means a live token that refreshes
// and a daemon polling on a schedule, which is observable behaviour a stale account would not show.
// So decoys hold integrations as PAUSED: present and configured, but not polling, which reads as a
// set-up-but-unused account. New integrations on a decoy default to Paused; on the main you enable
// them explicitly.
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
// integrations poll; a Paused one (the decoy default) sits silent.
func (i Integration) Polls() bool { return i.State == Enabled }

var (
	ErrNotFound          = errors.New("integration not found")
	ErrExists            = errors.New("integration already exists")
	ErrDecoyStaysPaused  = errors.New("decoy integrations stay paused; a live connector on a decoy is a behavioural tell")
)
