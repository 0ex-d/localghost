package profile

import (
	"github.com/LocalGhostDao/localghost/server/auth"
	"github.com/LocalGhostDao/localghost/server/wipe"
)

// Outcome is what an unlock attempt resulted in. Open looks identical to the user whether it is the
// main account or a decoy, so a coercer cannot tell which they got.
type Outcome int

const (
	Reject    Outcome = iota // invalid PIN, rate-limited; show "wrong PIN"
	Open                     // open OpenSlot's account (main or decoy)
	Throttled                // refused by the rate limiter (locked out / too soon)
)

// Decision is returned by Unlock. For Open, mount OpenSlot's container. MainWiped is true when this
// unlock was a duress decoy that crypto-erased the main account (for internal audit only; it must
// not change what the user sees).
type Decision struct {
	Outcome   Outcome
	OpenSlot  int
	MainWiped bool
	Err       error // for Throttled: ErrLockedOut / ErrTooSoon
}

// Accounts ties the registry, rate limiter, and wiper together. Unlock is the single entry point the
// daemon calls for every PIN attempt from a device.
type Accounts struct {
	reg   *Registry
	gate  *auth.Gate
	wiper *wipe.Wiper
}

func NewAccounts(reg *Registry, gate *auth.Gate, wiper *wipe.Wiper) *Accounts {
	return &Accounts{reg: reg, gate: gate, wiper: wiper}
}

// Unlock evaluates one PIN attempt from device id. Order: rate-limit gate, resolve, act. A valid PIN
// resets the limiter; an invalid one is recorded so brute force escalates and locks out. A duress
// decoy fires the main-account wipe here, before anything is returned, then opens its decoy exactly
// like a normal account so the result is indistinguishable.
func (a *Accounts) Unlock(id, pin string) Decision {
	if err := a.gate.CheckAllowed(id); err != nil {
		return Decision{Outcome: Throttled, Err: err}
	}

	res := a.reg.Resolve(pin)
	if !res.Valid {
		a.gate.RecordFailure(id)
		return Decision{Outcome: Reject}
	}
	a.gate.RecordSuccess(id)

	wiped := false
	switch {
	case res.Wipe == WipeAll:
		// Global wipe PIN (if one is configured): destroy every account, then look like a wrong PIN
		// so an onlooker sees nothing. The 3-slot default policy does not set one, but the registry
		// supports it, so handle it here rather than letting WipeAll fall through to a normal open.
		_ = a.wiper.PanicWipe(allSlots())
		return Decision{Outcome: Reject, MainWiped: true}
	case res.Wipe >= 0:
		// Duress decoy: crypto-erase the main account, then open the decoy normally.
		_ = a.wiper.WipeAccount(res.Wipe)
		wiped = true
	}
	return Decision{Outcome: Open, OpenSlot: res.Open, MainWiped: wiped}
}

// allSlots is the set of data slots a global wipe erases.
func allSlots() []int {
	out := make([]int, TotalSlots)
	for i := range out {
		out[i] = i
	}
	return out
}
