package admin

import (
	"errors"
	"fmt"
)

// resetup rotates a slot's PIN, which in this design means: destroy the slot's old key (the data is
// gone, crypto-erased) and create a fresh encrypted volume keyed by a new PIN. There is no
// rotate-in-place; a PIN reset IS a slot wipe. So resetup is reachable ONLY from the box over a
// local-network session (RequireLocal), never from the app, so no coerced phone can trigger it.
//
// Commands name the slot explicitly , resetup-main / resetup-decoy / resetup-wipe , and touch ONLY
// that slot. No relative roles, no cross-slot indirection.

// Slot is the named target. The mapping to physical slot index is fixed and absolute here (this is
// the box console, not the app), which is what keeps "resetup-decoy" from ever resolving to main.
// The roles match profile/setup.go's 3-slot policy: slot 0 main, slot 1 a plain decoy, slot 2 a
// duress decoy (opens believable data and wipes the main on unlock).
type Slot int

const (
	SlotMain   Slot = 0 // your real data
	SlotDecoy  Slot = 1 // plain decoy, no wipe
	SlotDuress Slot = 2 // duress decoy: opens believable data, wipes main on unlock
)

func (s Slot) String() string {
	switch s {
	case SlotMain:
		return "main"
	case SlotDecoy:
		return "decoy"
	case SlotDuress:
		return "duress"
	default:
		return "unknown"
	}
}

// Backend is what resetup needs from the box: report a slot's size (for the warning), destroy its
// key + volume, and create a fresh volume keyed by the new PIN. Implemented against the real
// wipe/container/profile machinery; an interface here so the ORDER and the safety are testable.
type Backend interface {
	SlotSizeBytes(slot Slot) (uint64, error)
	// CommitReset destroys the old key/volume and creates a fresh one keyed by newPin, all under the
	// new PIN. It is called ONLY after the new PIN is captured and confirmed.
	CommitReset(slot Slot, newPin string) error
	// ClearPinMemory zeroises any transient copy of a PIN we handled.
	ClearPinMemory()
}

var (
	ErrNotConfirmed = errors.New("new PIN was not confirmed; nothing was changed")
	ErrEmptyPin     = errors.New("new PIN must not be empty")
	ErrMismatch     = errors.New("the two new PINs did not match; nothing was changed")
)

// Warning is shown to the operator before anything is destroyed. SizeBytes lets the message say
// exactly how much is about to be erased.
type Warning struct {
	Slot      Slot
	SizeBytes uint64
}

func (w Warning) Message() string {
	return fmt.Sprintf(
		"You are about to ERASE the %s partition (%s). This is permanent , the current data cannot "+
			"be recovered. Enter and confirm the new PIN to proceed.",
		w.Slot, humanBytes(w.SizeBytes))
}

// Resetup orchestrates one rotation with the safe ordering. The crucial property: the old key is
// destroyed ONLY after the new PIN is entered AND confirmed (entered twice and matching). A typo at
// the new-PIN step aborts and leaves the slot untouched, so a slip never costs the volume. The
// window with neither old nor new key does not exist , CommitReset both wipes and re-creates atomically.
type Resetup struct {
	backend Backend
}

func NewResetup(b Backend) *Resetup { return &Resetup{backend: b} }

// Prepare runs the local-session gate and returns the warning to display. It touches nothing. If the
// session is not local, it refuses here, before any prompt.
func (r *Resetup) Prepare(slot Slot, connPeer, sshClientEnv string) (Warning, error) {
	if err := RequireLocal(connPeer, sshClientEnv); err != nil {
		return Warning{}, err
	}
	size, err := r.backend.SlotSizeBytes(slot)
	if err != nil {
		return Warning{}, err
	}
	return Warning{Slot: slot, SizeBytes: size}, nil
}

// Commit performs the rotation AFTER the operator has seen the warning and entered the new PIN twice.
// It re-checks locality (defence in depth), validates the two PINs match and are non-empty, and only
// then destroys the old key and creates the fresh volume. Order is the safety: confirm first,
// destroy second.
func (r *Resetup) Commit(slot Slot, newPin, confirmPin, connPeer, sshClientEnv string) error {
	if err := RequireLocal(connPeer, sshClientEnv); err != nil {
		return err
	}
	defer r.backend.ClearPinMemory()
	if newPin == "" {
		return ErrEmptyPin
	}
	if newPin != confirmPin {
		return ErrMismatch
	}
	// Confirmed. NOW destroy the old key and create the fresh volume keyed by the new PIN.
	return r.backend.CommitReset(slot, newPin)
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
