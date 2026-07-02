//go:build tpm

package hw

// Dictionary-attack lockout is the REAL anti-brute-force wall on the first-unlock-per-boot unseal
// path , the one root cannot reset without the lockout authorization. The software limiter in
// ghost.secd no longer walls anything; it only absorbs repeated-identical PINs so they never reach
// the TPM as fresh attempts. So the numbers here ARE the brute-force budget for the security-
// critical path, and they are set once at provision.
//
// The lockout authorization is set to pinAuth(pin) , the SAME value already used as each sealed
// object's authValue (see tpm.go). That keeps it to one secret the owner must remember (the PIN),
// introduces no new stored credential, and means a change-PIN must re-key this auth too (old->new)
// while a resetup, having lost the PIN, must go through `tpm2 clear -c platform` to reset the
// lockout hierarchy before re-provisioning.
//
// GLOBAL, not per-app: TPM 2.0 has ONE dictionary-attack counter for the whole device. This is safe
// here only because LocalGhost is the sole TPM tenant on the box; ForeignPersistentHandles is the
// check that guards that assumption before we touch the global policy.
//
// NOT validated in CI here (no TPM in the build env). Built against the go-tpm command API; the
// exact capability-response field names must be confirmed against the pinned go-tpm on the box:
// go test -tags tpm ./internal/hw against /dev/tpmrm0.

import (
	"fmt"

	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpm2/transport"
)

// DA policy. 5 distinct wrong tries, then the TPM locks; a failed-attempt entry ages out after
// recoveryTime, and a full lockout clears after lockoutRecoveryTime. At 5 tries per 24h a 6-digit
// PIN is ~9,600 years in expectation; even a 4-digit PIN is centuries. Tune here if desired.
const (
	daMaxTries            = 5
	daRecoverySec         = 24 * 60 * 60 // an accumulated failure ages out after this
	daLockoutRecoverySec  = 24 * 60 * 60 // after a full lockout, wait this before lockout auth is usable
)

// Our persisted objects live at 0x81010000+slot (see NewTPMSealedKey); the parent is transient. Any
// persistent handle outside this window is another tenant's , which the sole-tenant check flags.
const (
	ourHandleLo uint32 = 0x81010000
	ourHandleHi uint32 = 0x8101000F
)

// ForeignPersistentHandles returns every persistent handle on the TPM that LocalGhost did not
// create. An empty slice means we are the sole tenant and it is safe to set the GLOBAL DA policy.
// A non-empty slice is not fatal by itself , the operator decides , but setting a tight global
// lockout (and any future `tpm2 clear` during resetup) would affect those objects too.
func ForeignPersistentHandles(device string) ([]uint32, error) {
	tpm, err := transport.OpenTPM(device)
	if err != nil {
		return nil, fmt.Errorf("open tpm: %w", err)
	}
	defer tpm.Close()

	rsp, err := tpm2.GetCapability{
		Capability:    tpm2.TPMCapHandles,
		Property:      uint32(tpm2.TPMHTPersistent) << 24, // start of the persistent range
		PropertyCount: 128,
	}.Execute(tpm)
	if err != nil {
		return nil, fmt.Errorf("get persistent handles: %w", err)
	}
	handles, err := rsp.CapabilityData.Data.Handles()
	if err != nil {
		return nil, fmt.Errorf("decode handle list: %w", err)
	}

	var foreign []uint32
	for _, h := range handles.Handle {
		v := uint32(h)
		if v < ourHandleLo || v > ourHandleHi {
			foreign = append(foreign, v)
		}
	}
	return foreign, nil
}

// SetupLockout binds the lockout hierarchy to pinAuth(pin) and applies the DA policy. It is
// idempotent: on a fresh TPM the lockout auth is empty and we set it; on a re-run it is already
// pinAuth(pin) and we simply re-apply the parameters. If it is set to anything else, we refuse
// rather than guess , that box needs `tpm2 clear -c platform` (the resetup path) first.
func SetupLockout(device, pin string) error {
	want := pinAuth(pin)

	tpm, err := transport.OpenTPM(device)
	if err != nil {
		return fmt.Errorf("open tpm: %w", err)
	}
	defer tpm.Close()

	// Try to claim the lockout auth from empty (the fresh-TPM case).
	_, changeErr := tpm2.HierarchyChangeAuth{
		AuthHandle: tpm2.AuthHandle{
			Handle: tpm2.TPMRHLockout,
			Auth:   tpm2.PasswordAuth(nil), // empty on a fresh TPM
		},
		NewAuth: tpm2.TPM2BAuth{Buffer: want},
	}.Execute(tpm)

	if changeErr != nil {
		// Already set to something. Confirm it is OURS by using `want` to apply DA params; if that
		// works, this is an idempotent re-run. If it does not, the lockout auth is foreign/unknown.
		if daErr := applyDAParams(tpm, want); daErr != nil {
			return fmt.Errorf(
				"TPM lockout auth is already set to a value that is not pinAuth(PIN); "+
					"reset it via `tpm2 clear -c platform` (resetup) before provisioning: change=%v apply=%v",
				changeErr, daErr)
		}
		return nil // idempotent: auth already ours, params re-applied
	}

	// Fresh set succeeded; now apply the DA parameters authorised by the new auth.
	if err := applyDAParams(tpm, want); err != nil {
		return fmt.Errorf("set DA parameters: %w", err)
	}
	return nil
}

// applyDAParams sets maxTries/recovery/lockoutRecovery, authorised by the lockout auth.
func applyDAParams(tpm transport.TPMCloser, lockoutAuth []byte) error {
	_, err := tpm2.DictionaryAttackParameters{
		LockHandle: tpm2.AuthHandle{
			Handle: tpm2.TPMRHLockout,
			Auth:   tpm2.PasswordAuth(lockoutAuth),
		},
		NewMaxTries:     daMaxTries,
		NewRecoveryTime: daRecoverySec,
		LockoutRecovery: daLockoutRecoverySec,
	}.Execute(tpm)
	return err
}

// ChangeLockoutAuth re-keys the lockout hierarchy from pinAuth(old) to pinAuth(new). Call this from
// the change-PIN path in the SAME operation as the reseal, so the lockout auth never drifts away
// from the PIN. If it fails, the caller must roll the reseal back.
func ChangeLockoutAuth(device, oldPin, newPin string) error {
	tpm, err := transport.OpenTPM(device)
	if err != nil {
		return fmt.Errorf("open tpm: %w", err)
	}
	defer tpm.Close()

	_, err = tpm2.HierarchyChangeAuth{
		AuthHandle: tpm2.AuthHandle{
			Handle: tpm2.TPMRHLockout,
			Auth:   tpm2.PasswordAuth(pinAuth(oldPin)),
		},
		NewAuth: tpm2.TPM2BAuth{Buffer: pinAuth(newPin)},
	}.Execute(tpm)
	if err != nil {
		return fmt.Errorf("re-key lockout auth: %w", err)
	}
	return nil
}
