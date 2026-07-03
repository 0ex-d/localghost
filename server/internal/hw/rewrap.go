package hw

// Tier migration: re-wrap the SAME AMK under the other tier. The disk is never touched , the LUKS
// key does not change, only its wrapping does , so migrate-to-tpm after a firmware clear (or
// migrate-to-software before pulling a TPM) is a key-management operation, not a re-encryption.
//
// Crash-safety ordering, which the callers (ghost-ctl migrate-*) must follow:
//
//	1. ReWrap: unseal from the current tier, seal into the target tier, VERIFY the target tier
//	   unseals the identical AMK. After this, BOTH wrappings exist and are valid.
//	2. Flip GHOST_SEAL_MODE to the target. This is the commit point.
//	3. Destroy the old tier's wrapping (TPM evict, or delete the software slots + salt).
//
// A crash at any point leaves the box unlockable: before the flip, the mode points at the old
// wrapping (still present); after the flip, it points at the new one (verified before the flip).
// Only after the flip is the old wrapping removed. Never destroy before verifying and committing.

import (
	"crypto/subtle"
	"fmt"
)

// ReWrap moves the AMK from one sealer to another under the same PIN, verifying the target wrapping
// recovers the identical AMK before returning. It does NOT flip the mode or destroy the old
// wrapping , the caller commits in the documented order. A wrong PIN surfaces as the current tier's
// unseal error (ErrWrongPIN on software; the TPM's DA-lockout-charged failure on hardware , so
// callers should warn that a wrong PIN on a TPM box spends lockout budget).
func ReWrap(from, to Sealer, pin string) error {
	amk, err := from.Unseal(pin)
	if err != nil {
		return fmt.Errorf("unseal from current tier: %w", err)
	}
	defer zeroize(amk)

	if err := to.Seal(pin, amk); err != nil {
		return fmt.Errorf("seal into target tier: %w", err)
	}

	// Verify: the target must give back the exact AMK, or the migration is aborted with the old
	// wrapping untouched. Constant-time compare out of habit; nothing here is attacker-observable.
	back, err := to.Unseal(pin)
	if err != nil {
		return fmt.Errorf("verify target tier unseal: %w", err)
	}
	defer zeroize(back)
	if subtle.ConstantTimeCompare(amk, back) != 1 {
		return fmt.Errorf("target tier returned a different key on verify; migration aborted, current tier untouched")
	}
	return nil
}
