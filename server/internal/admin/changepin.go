package admin

import "errors"

// Change-PIN is the non-destructive counterpart to resetup. Resetup ("I lost the PIN") wipes the
// slot; change-PIN ("I know the PIN, I want a new one") keeps the data. The data is NOT re-encrypted:
// the volume is encrypted under a random data key, and the PIN only wraps that key. So changing a PIN
// is unwrap-the-data-key-with-the-old-PIN, re-wrap-under-the-new-PIN, write back the few hundred
// bytes of wrapped key. The bulk data never moves , seconds, not hours.
//
// Like resetup it is box-only over a local-network session. The current PIN is a second factor on
// top of that, not a replacement: requiring the box means a coercer with only your unlocked phone
// cannot change your PIN to one they choose and lock you out.

// ChangePinBackend re-wraps a slot's data key under a new PIN. The current PIN authenticates by
// unwrapping: a wrong current PIN fails the unwrap, so the slot self-authenticates. Implemented over
// the vault's WrapDataKey/UnwrapDataKey, so "the data key never moves" is structural.
type ChangePinBackend interface {
	// RewrapKey unwraps the slot's data key with oldPin and re-wraps it under newPin, writing the new
	// wrapped blob ONLY AFTER it is produced, then dropping the old. Returns ErrWrongPin if oldPin
	// does not unwrap. Touches only this slot's wrapped-key blob, never the bulk data.
	RewrapKey(slot Slot, oldPin, newPin string) error
	ClearPinMemory()
}

var (
	ErrWrongPin    = errors.New("current PIN is incorrect; nothing was changed")
	ErrSamePin     = errors.New("new PIN is the same as the current one")
)

// ChangePin orchestrates one non-destructive rotation.
type ChangePin struct {
	backend ChangePinBackend
}

func NewChangePin(b ChangePinBackend) *ChangePin { return &ChangePin{backend: b} }

// Run changes a slot's PIN. It re-checks locality (defence in depth), validates the new PIN was
// entered twice and differs from the old, then re-wraps. The current PIN both authorises and
// authenticates: it must unwrap the slot's data key or the whole thing fails with ErrWrongPin and
// nothing changes. Authority is per-slot , holding slot N's current PIN authorises changing slot N
// only.
func (c *ChangePin) Run(slot Slot, oldPin, newPin, confirmPin, connPeer, sshClientEnv string) error {
	if err := RequireLocal(connPeer, sshClientEnv); err != nil {
		return err
	}
	defer c.backend.ClearPinMemory()
	if oldPin == "" || newPin == "" {
		return ErrEmptyPin
	}
	if newPin != confirmPin {
		return ErrMismatch
	}
	if newPin == oldPin {
		return ErrSamePin
	}
	// RewrapKey verifies oldPin (by unwrapping) and writes the new blob before dropping the old, so a
	// wrong old PIN changes nothing and a crash mid-op cannot leave the slot unopenable.
	return c.backend.RewrapKey(slot, oldPin, newPin)
}
