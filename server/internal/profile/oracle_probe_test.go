package profile

import (
	"sort"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/auth"
	"github.com/LocalGhostDao/localghost/server/internal/wipe"
)

func quickGateAccounts(t *testing.T) *Accounts {
	t.Helper()
	s, err := NewSetup([]byte("box-salt"))
	must(t, err)
	must(t, s.SetMain("1111"))
	must(t, s.SetWipe("3333"))
	reg, err := s.Finalize()
	must(t, err)
	pol := auth.DefaultPolicy()
	pol.BaseDelay = 150 * time.Millisecond // the default's shape, faster
	return NewAccounts(reg, auth.NewGate(pol, auth.NewMemoryStore()), wipe.NewWiper(wipe.NewKeyVault(), nil, nil))
}

// PROBE (free-time audit, 30 Sep 2026): the wipe PIN presents as a wrong PIN, but does the rate
// limiter treat it as one? Three wrong guesses, then a candidate, then one more guess at once:
// if the candidate was the wipe PIN, the counter was reset and the next guess is evaluated (the
// KDF runs, ~Argon2 time); if it was wrong, the next guess is throttled (answered at once).
func TestProbeGateTellsTheWipePin(t *testing.T) {
	try := func(candidate string) (Outcome, time.Duration) {
		a := quickGateAccounts(t)
		for i := 0; i < 3; i++ {
			a.Unlock("dev", "0000")
		}
		time.Sleep(200 * time.Millisecond) // wait out the first cooldown
		a.Unlock("dev", candidate)
		t0 := time.Now()
		d := a.Unlock("dev", "9999")
		return d.Outcome, time.Since(t0)
	}
	oWipe, tWipe := try("3333")
	oWrong, tWrong := try("5555")
	t.Logf("after the wipe PIN: next guess outcome=%v in %v", oWipe, tWipe.Round(time.Millisecond))
	t.Logf("after a wrong PIN: next guess outcome=%v in %v", oWrong, tWrong.Round(time.Millisecond))
	if oWipe != oWrong {
		t.Logf("TELL: the limiter treats the wipe PIN differently from a wrong one")
	}
}

// After the fix: the wipe PIN counts like a wrong PIN, so the guess after it is treated the same
// either way; and a throttled guess takes the KDF's time like an evaluated one.
func TestGateTreatsTheWipePinAsWrong(t *testing.T) {
	next := func(candidate string) (Outcome, time.Duration) {
		a := quickGateAccounts(t)
		for i := 0; i < 3; i++ {
			a.Unlock("dev", "0000")
		}
		time.Sleep(200 * time.Millisecond)
		a.Unlock("dev", candidate)
		t0 := time.Now()
		d := a.Unlock("dev", "9999")
		return d.Outcome, time.Since(t0)
	}
	// three runs each, the median kept: one KDF run on a busy machine (a CI runner, a laptop
	// indexing) can take twice another, and that is noise, not a tell
	median := func(candidate string) (Outcome, time.Duration) {
		var o Outcome
		var ds []time.Duration
		for i := 0; i < 3; i++ {
			oi, d := next(candidate)
			if i > 0 && oi != o {
				t.Fatalf("outcome after %s varies: %v then %v", candidate, o, oi)
			}
			o = oi
			ds = append(ds, d)
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		return o, ds[1]
	}
	oWipe, tWipe := median("3333")
	oWrong, tWrong := median("5555")
	if oWipe != oWrong {
		t.Fatalf("the limiter still tells: after wipe %v, after wrong %v", oWipe, oWrong)
	}
	// both ran the KDF: within 60% of each other, and neither near zero
	lo, hi := tWipe, tWrong
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo < 20*time.Millisecond || float64(hi) > 1.6*float64(lo) {
		t.Fatalf("times differ: after wipe %v, after wrong %v", tWipe, tWrong)
	}
}

// The owner's confirmation still works after a coercer's run of wrong guesses: the wipe PIN, typed
// once the cooldown allows, is the twelfth failure and locks the limiter for an hour, and the main
// PIN still confirms the armed wipe through that lockout.
func TestWipeConfirmsThroughTheLimiter(t *testing.T) {
	s, err := NewSetup([]byte("box-salt"))
	must(t, err)
	must(t, s.SetMain("1111"))
	must(t, s.SetWipe("3333"))
	reg, err := s.Finalize()
	must(t, err)
	pol := auth.DefaultPolicy()
	pol.BaseDelay, pol.MaxDelay = time.Millisecond, 20*time.Millisecond
	a := NewAccounts(reg, auth.NewGate(pol, auth.NewMemoryStore()), wipe.NewWiper(wipe.NewKeyVault(), nil, nil))
	for i := 0; i < 11; i++ {
		time.Sleep(25 * time.Millisecond)
		a.Unlock("dev", "0000")
	}
	time.Sleep(25 * time.Millisecond)
	if d := a.Unlock("dev", "3333"); d.Outcome != Reject {
		t.Fatalf("the wipe PIN was not taken: %+v", d)
	}
	if d := a.Unlock("dev", "9999"); d.Outcome != Throttled {
		t.Fatalf("twelve failures should lock the limiter: %+v", d)
	}
	if d := a.Unlock("dev", "1111"); !d.Wiped {
		t.Fatalf("the confirmation was held off by the limiter: %+v", d)
	}
}
