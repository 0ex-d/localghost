package profile

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
)

// MaxSlots is the cap on real profiles. RegistrySize is the FIXED number of entries the registry
// always holds on disk, real ones padded with random filler, so the number of real PINs never
// leaks. An attacker reading the registry sees RegistrySize identical-looking entries and cannot
// tell how many correspond to a memorised PIN. That hidden count is what lets you hand over a
// couple of decoy PINs under coercion and deny that any others exist.
const (
	MaxSlots     = 5
	RegistrySize = 10 // room for up to 5 profiles plus duress entries, padded with random
	hashLen      = 32
)

// entry binds a PIN (by hash) to what it does: open one slot, and optionally wipe another. A normal
// profile has wipe == NoSlot. A duress profile opens a decoy slot and wipes the main slot. Filler
// entries have a random hash no PIN will ever match.
// NoSlot marks "no slot" for the open or wipe field. WipeAll marks a global wipe (the dedicated
// wipe PIN destroys every account, versus a duress PIN which wipes only the main slot).
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
// rate-limits). For a valid PIN, Open is the slot to open and Wipe is the slot to crypto-erase on
// this unlock (NoSlot for a normal profile).
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
	// real count is hidden even before any profile is added.
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
	ErrSlotInUse = errors.New("slot already has a profile")
	ErrPinReused = errors.New("PIN already registered")
)

// AddProfile registers a normal profile PIN for a slot.
func (r *Registry) AddProfile(pin string, slot int) error {
	return r.add(pin, slot, NoSlot, true)
}

// AddDuress registers a duress PIN: entering it opens decoySlot (believable data) and crypto-erases
// mainSlot. The decoy must be a real, pre-populated profile slot so it looks convincing.
func (r *Registry) AddDuress(pin string, decoySlot, mainSlot int) error {
	if mainSlot < 0 || mainSlot >= MaxSlots {
		return fmt.Errorf("main slot out of range")
	}
	return r.add(pin, decoySlot, mainSlot, false)
}

// SetWipePin registers the dedicated global wipe PIN: entering it destroys every account. It opens
// no profile (Resolve returns Open == NoSlot), so the caller shows the same response as a wrong PIN
// while everything is erased in the background.
func (r *Registry) SetWipePin(pin string) error {
	if r.used >= RegistrySize {
		return ErrFull
	}
	h := PinKey(pin, r.salt)
	for i := 0; i < r.used; i++ {
		if subtle.ConstantTimeCompare(r.entries[i].hash, h) == 1 {
			return ErrPinReused
		}
	}
	r.entries[r.used] = entry{hash: h, open: NoSlot, wipe: WipeAll}
	r.used++
	return nil
}

func (r *Registry) add(pin string, open, wipe int, checkSlot bool) error {
	if open < 0 || open >= MaxSlots {
		return fmt.Errorf("open slot out of range: %d", open)
	}
	if r.used >= RegistrySize {
		return ErrFull
	}
	h := PinKey(pin, r.salt)
	for i := 0; i < r.used; i++ {
		if subtle.ConstantTimeCompare(r.entries[i].hash, h) == 1 {
			return ErrPinReused
		}
		if checkSlot && r.entries[i].open == open && r.entries[i].wipe == NoSlot {
			return ErrSlotInUse
		}
	}
	// Overwrite the next random-filler entry with the real one.
	r.entries[r.used] = entry{hash: h, open: open, wipe: wipe}
	r.used++
	return nil
}

// Resolve checks a PIN against ALL RegistrySize entries in constant time (real and filler alike),
// with no short-circuit, so neither timing nor comparison count reveals how many real PINs exist or
// which one matched.
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
