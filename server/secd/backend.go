package secd

import "github.com/LocalGhostDao/localghost/server/profile"

// UnlockBackend is what the unlock flow needs from the box to turn a PIN into a mounted, running
// account. It is the single seam between the HTTP server and the hardware: the default build wires a
// simulation (so ghost.secd compiles and the app flow is testable with no TPM), and the `tpm` build
// wires the real TPM + dm-crypt + per-account Postgres/Redis path.
//
// The flow mirrors the unlock stages exactly:
//   Resolve  decide what the PIN means (real account, decoy, duress-wipe, or reject)
//   Unseal   ask the TPM to release the account's master key for the resolved slot (PIN-bound; the
//            TPM enforces its own lockout, so a wrong PIN is punished in hardware)
//   Mount    dm-crypt map + filesystem mount of that slot's container with the unsealed key
//   StartDB  bring up the account's Postgres inside the mounted volume
//   StartCache bring up the account's Redis inside the mounted volume
//
// A duress PIN resolves to a decoy slot AND triggers the main account's crypto-erase; the backend
// performs the erase during Resolve so the stage timing is identical to a normal unlock (an onlooker
// cannot tell a duress unlock from a real one).
type UnlockBackend interface {
	// Resolve maps the PIN to an outcome. It returns the slot to open (or NoSlot on reject) and
	// whether the main account was wiped (duress). It must run in constant-ish time regardless of
	// outcome so timing does not leak which PIN was entered.
	Resolve(pin string) (openSlot int, mainWiped bool, err error)

	// Unseal releases the slot's master key from the TPM. The PIN is supplied again as the TPM auth
	// value; the hardware checks it and enforces the dictionary-attack lockout.
	Unseal(slot int, pin string) (key []byte, err error)

	// Mount maps and mounts the slot's encrypted container using key. Returns when the filesystem is
	// ready. key is zeroised by the caller after this returns.
	Mount(slot int, key []byte) error

	// StartDB and StartCache bring up the per-account Postgres and Redis inside the mounted volume.
	StartDB(slot int) error
	StartCache(slot int) error

	// Warm reports whether the slot is already mounted and running (a hot unlock), so the heavy
	// stages report Skipped instead of re-running.
	Warm(slot int) bool
}

// runUnlock drives the backend through the unlock stages, emitting progress. It is shared by both
// backend builds so the stage sequence and timing-uniformity logic live in one place. The PIN is
// resolved first (the security decision); a reject ends the stream with a failure. The key is
// zeroised immediately after Mount consumes it.
func runUnlock(b UnlockBackend, pin string, emit func(profile.Progress)) (openSlot int, err error) {
	openSlot = profile.NoSlot

	// RESOLVE
	emit(profile.Progress{Stage: profile.StageResolve, State: profile.Running})
	slot, _, rerr := b.Resolve(pin)
	if rerr != nil || slot == profile.NoSlot {
		emit(profile.Progress{Stage: profile.StageResolve, State: profile.Errored})
		if rerr != nil {
			return profile.NoSlot, rerr
		}
		return profile.NoSlot, errReject
	}
	emit(profile.Progress{Stage: profile.StageResolve, State: profile.Complete})

	warm := b.Warm(slot)
	heavy := func(stage profile.Stage, do func() error) error {
		if warm {
			emit(profile.Progress{Stage: stage, State: profile.Skipped})
			return nil
		}
		emit(profile.Progress{Stage: stage, State: profile.Running})
		if err := do(); err != nil {
			emit(profile.Progress{Stage: stage, State: profile.Errored})
			return err
		}
		emit(profile.Progress{Stage: stage, State: profile.Complete})
		return nil
	}

	// UNSEAL (always runs even when warm, but cheap; the key is needed only for a cold mount)
	emit(profile.Progress{Stage: profile.StageUnseal, State: profile.Running})
	key, uerr := b.Unseal(slot, pin)
	if uerr != nil {
		emit(profile.Progress{Stage: profile.StageUnseal, State: profile.Errored})
		return profile.NoSlot, uerr
	}
	emit(profile.Progress{Stage: profile.StageUnseal, State: profile.Complete})

	// MOUNT (consumes + zeroises the key)
	if err := heavy(profile.StageMount, func() error { return b.Mount(slot, key) }); err != nil {
		zeroise(key)
		return profile.NoSlot, err
	}
	zeroise(key)

	// START_DB, START_CACHE
	if err := heavy(profile.StageStartDB, func() error { return b.StartDB(slot) }); err != nil {
		return profile.NoSlot, err
	}
	if err := heavy(profile.StageStartCache, func() error { return b.StartCache(slot) }); err != nil {
		return profile.NoSlot, err
	}

	// DAEMONS, READY
	emit(profile.Progress{Stage: profile.StageDaemons, State: profile.Complete})
	emit(profile.Progress{Stage: profile.StageReady, State: profile.Complete})
	return slot, nil
}

func zeroise(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
