package profile

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
)

// The box holds ONE real account (slot 0). RegistrySize is the FIXED number of entries the registry
// always holds on disk, the real ones (main PIN, and optionally a wipe PIN) padded with random
// filler. An attacker reading the registry sees RegistrySize identical-looking entries and cannot
// tell how many correspond to a real PIN, which one opens the account, or which one wipes it , or
// even that a wipe PIN exists at all. That indistinguishability is the whole point of the padding:
// the wipe PIN must not be identifiable as the wipe PIN from the stored form.
const (
	RegistrySize = 10 // real entries (main [+ wipe]) padded with random filler
	hashLen      = 32
)

// entry binds a PIN (by hash) to what it does: open a slot, and/or wipe. The main PIN opens slot 0
// (wipe == NoSlot). The wipe PIN opens nothing and wipes everything (open == NoSlot, wipe ==
// WipeAll). Filler entries have a random hash no PIN will ever match. All three kinds are the same
// shape on disk, so the stored form never reveals which entry is which.
//
// NoSlot marks "no slot" for the open or wipe field. WipeAll marks the global crypto-erase.
const (
	NoSlot  = -1
	WipeAll = -2
)

type entry struct {
	hash []byte
	open int
	wipe int
}

// Resolution is what Resolve returns. Valid is false for an unknown PIN (the caller rejects and
// rate-limits). For a valid PIN, Open is the slot to open (NoSlot for the wipe PIN) and Wipe is
// WipeAll when this PIN triggers the global crypto-erase.
type Resolution struct {
	Valid bool
	Open  int
	Wipe  int
}

// Registry resolves a PIN in constant time against a fixed number of entries.
type Registry struct {
	salt    []byte
	entries [RegistrySize]entry
	used    int // how many real entries are filled; not persisted in the clear
}

func NewRegistry(boxSalt []byte) (*Registry, error) {
	r := &Registry{salt: boxSalt}
	// Pre-fill every slot with a random hash so the on-disk form is full from the start and the
	// real count is hidden even before any PIN is added.
	for i := range r.entries {
		h := make([]byte, hashLen)
		if _, err := rand.Read(h); err != nil {
			return nil, err
		}
		r.entries[i] = entry{hash: h, open: NoSlot, wipe: NoSlot}
	}
	return r, nil
}

var (
	ErrFull      = errors.New("registry is full")
	ErrPinReused = errors.New("PIN already registered")
)

// AddProfile registers the main account PIN for a slot (slot 0 in the single-account model).
func (r *Registry) AddProfile(pin string, slot int) error {
	return r.add(pin, slot, NoSlot)
}

// SetWipePin registers the global wipe PIN: entering it crypto-erases everything. It opens no
// profile (Resolve returns Open == NoSlot), so the caller shows the SAME response as a wrong PIN
// while the erase runs , the act of wiping is indistinguishable from a failed unlock.
func (r *Registry) SetWipePin(pin string) error {
	return r.add(pin, NoSlot, WipeAll)
}

func (r *Registry) add(pin string, open, wipe int) error {
	if r.used >= RegistrySize {
		return ErrFull
	}
	h := PinKey(pin, r.salt)
	for i := 0; i < r.used; i++ {
		if subtle.ConstantTimeCompare(r.entries[i].hash, h) == 1 {
			return ErrPinReused
		}
	}
	// Overwrite the next random-filler entry with the real one.
	r.entries[r.used] = entry{hash: h, open: open, wipe: wipe}
	r.used++
	return nil
}

// Resolve checks a PIN against ALL RegistrySize entries in constant time (real and filler alike),
// with no short-circuit, so neither timing nor comparison count reveals how many real PINs exist,
// which one matched, or whether the matched PIN opens or wipes.
func (r *Registry) Resolve(pin string) Resolution {
	cand := PinKey(pin, r.salt)
	valid := 0
	open := NoSlot
	wipe := NoSlot
	for i := range r.entries {
		m := subtle.ConstantTimeCompare(r.entries[i].hash, cand)
		valid = subtle.ConstantTimeSelect(m, 1, valid)
		open = subtle.ConstantTimeSelect(m, r.entries[i].open, open)
		wipe = subtle.ConstantTimeSelect(m, r.entries[i].wipe, wipe)
	}
	return Resolution{Valid: valid == 1, Open: open, Wipe: wipe}
}
