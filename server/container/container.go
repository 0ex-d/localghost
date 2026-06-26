package container

import (
	"errors"
	"fmt"
)

// A container is one account's encrypted store, fixed-size so no account looks bigger than another.
// The real account fills its container with data plus padding; the decoys fill the SAME size with
// believable data plus padding. From outside, every container is an identical-size blob of
// ciphertext, and the internal fill level (90% vs 5%) is invisible because it is inside the
// encryption. That equality is what stops "the biggest one is the real one".
//
// This package defines the size invariant and the mount seam. The actual encrypted block device
// (dm-crypt / LUKS over a fixed-size backing file or LV, keyed by the TPM-sealed account key) is the
// system integration, marked below.

// Spec is the shared shape of every container. All slots use the SAME Spec, so all are equal size.
type Spec struct {
	SizeBytes uint64 // identical for every slot
}

// Layout is the set of containers, one per slot. The invariant: every container is exactly
// Spec.SizeBytes on disk.
type Layout struct {
	Spec  Spec
	Slots map[int]Container
}

// Container is one fixed-size encrypted store.
type Container interface {
	Slot() int
	SizeBytes() uint64
}

var (
	ErrUnequalSizes = errors.New("containers are not all the same size; the larger one would reveal the real account")
	ErrWrongSize    = errors.New("container size does not match the shared spec")
)

// Verify enforces the deniability invariant: every container is present and exactly the spec size.
// Call it after setup and after any grow operation. A single odd-sized container breaks deniability,
// so this is a hard check, not a warning.
func (l Layout) Verify(expectSlots int) error {
	if len(l.Slots) != expectSlots {
		return fmt.Errorf("expected %d containers, found %d", expectSlots, len(l.Slots))
	}
	for slot, c := range l.Slots {
		if c.SizeBytes() != l.Spec.SizeBytes {
			return fmt.Errorf("slot %d: %w (%d != %d)", slot, ErrWrongSize, c.SizeBytes(), l.Spec.SizeBytes)
		}
	}
	return nil
}

// GrowAll raises every container to newSize by APPENDING RANDOM BYTES to each backing store. This
// is the only sanctioned way to add space, and it deliberately needs NO account key:
//
//   - Random bytes are indistinguishable from the ciphertext already in the container, so appending
//     them keeps every container an equal-size blob of opaque data. Growing only the one that filled
//     up would instantly reveal it.
//   - Because it is keyless, the daemon can grow all three in lockstep whenever the main account
//     fills, WITHOUT ever holding the decoy keys. The account keys stay fully independent: nothing
//     derives a decoy key from the main, and no account can unlock another. That independence is
//     what preserves deniability in both directions (coercing the main reveals nothing about the
//     decoys).
//   - The per-account filesystem only extends into the new space later, at mount, with that
//     account's own key (Mounter.ResizeToFill). So a decoy "catches up" its internal size privately
//     the next time its owner opens it.
func (l *Layout) GrowAll(newSize uint64, appendRandom func(slot int, extra uint64) error) error {
	if newSize < l.Spec.SizeBytes {
		return fmt.Errorf("cannot shrink containers")
	}
	extra := newSize - l.Spec.SizeBytes
	for slot := range l.Slots {
		if err := appendRandom(slot, extra); err != nil {
			return fmt.Errorf("growing slot %d: %w", slot, err)
		}
	}
	l.Spec.SizeBytes = newSize
	return nil
}

// Mounter mounts and unmounts a slot's container. The implementation:
//   - unseals the slot's account key from the TPM (PIN-gated; the TPM enforces the lockout),
//   - maps the fixed-size ciphertext as a block device (dm-crypt) WITHOUT bulk-decrypting it, so
//     mount time is the TPM unseal plus key setup, the SAME for every slot regardless of fill.
//
// Uniform mount time matters as much as uniform size: a slot that mounts faster because it holds
// less data would be a timing tell. The TPM unseal is slow but uniformly slow, which is fine.
//
// This is the system seam; the dm-crypt + TPM wiring needs the box to test. It is the next milestone
// alongside the TPM key-seal/erase used by the wipe package.
type Mounter interface {
	Mount(slot int, pin string) (mountPath string, err error)
	Unmount(slot int) error

	// ResizeToFill extends the account's filesystem to fill its (already-grown) container. It runs
	// at mount, with the account's OWN key, so a decoy looks full when its owner opens it. It is
	// strictly per-account: it never needs another account's key, which is what keeps the three
	// accounts cryptographically independent even though they grow in lockstep.
	ResizeToFill(slot int) error
}
