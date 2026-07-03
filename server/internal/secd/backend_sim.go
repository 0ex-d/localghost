//go:build !tpm

package secd

import (
	"crypto/sha256"
	"errors"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/profile"
	"golang.org/x/crypto/argon2"
)

// This is the DEFAULT (no-TPM) unlock backend. It lets ghost.secd compile and run, and the app's
// unlock flow work end to end, on any machine without a TPM or encrypted volumes. It accepts any
// non-empty PIN as the main account and simulates the cold-unlock cost so the stage stream is
// realistic. The real path lives in backend_tpm.go behind the `tpm` build tag; build with
// `-tags tpm` on the box to use the TPM + dm-crypt + Postgres/Redis backend.
//
// This simulation is NOT a security boundary , it opens the "main" slot for any PIN. It exists so
// development and the app's loading UI are exercisable off-box. Never run a real box without -tags tpm.

var errReject = errors.New("unlock rejected")

type simBackend struct{}

func newDefaultBackend(Config) UnlockBackend { return simBackend{} }

func (simBackend) Resolve(pin string) (int, bool, error) {
	if pin == "" {
		return profile.NoSlot, false, nil
	}
	return profile.MainSlot, false, nil
}

// Unseal returns the key the sim provisioning path used to format the container , derived from the
// PIN, matching debian.SimDiskKey. This keeps setup and daemon in agreement so a real-mount sim
// backend (see Mount's note) would open exactly what --insecure-sim wrote. It is NOT zeros anymore,
// because zeros only worked while Mount was a no-op; the moment anything actually opens the volume,
// the key must be the real one.
func (simBackend) Unseal(_ int, pin string) ([]byte, error) {
	return simDiskKey(pin), nil
}

// Mount in the sim backend SIMULATES the cold-unlock cost and does NOT open a real dm-crypt volume.
// Stated plainly so nobody is misled: --insecure-sim on ghost-setup writes a genuine PIN-keyed LUKS
// container to disk, but THIS backend does not mount it , it sleeps and returns. Provisioning is
// real; runtime mounting is simulated. Wiring a real cryptsetup-open here (using Unseal's key) plus
// real Postgres/Redis start is the same work as the tpm backend minus the seal, and is deliberately
// left undone until there is a reason to run an actually-mounting box without a TPM. The key
// agreement above means that day is a small change, not a redesign.
func (simBackend) Mount(int, []byte) error { time.Sleep(400 * time.Millisecond); return nil }
func (simBackend) StartDB(int) error                  { time.Sleep(300 * time.Millisecond); return nil }
func (simBackend) StartCache(int) error               { time.Sleep(100 * time.Millisecond); return nil }
func (simBackend) Warm(int) bool                      { return false }
func (simBackend) Lock(_ int, emit func(profile.Progress)) error {
	// Walk the same teardown stages as the real backend, with a small cost each, so the app's lock
	// UI is exercisable off-box.
	for _, st := range profile.LockStages {
		if st == profile.StageLocked {
			emit(profile.Progress{Stage: st, State: profile.Complete})
			continue
		}
		emit(profile.Progress{Stage: st, State: profile.Running})
		time.Sleep(120 * time.Millisecond)
		emit(profile.Progress{Stage: st, State: profile.Complete})
	}
	return nil
}

// simDiskKey MUST match debian.SimDiskKey byte for byte , setup formats the container with it and
// this backend would open the container with it. Duplicated rather than imported because
// internal/setup/debian pulls in provisioning-only deps the daemon should not link; the derivation
// is tiny and pinned by a cross-check test (TestSimDiskKeyMatchesSetup) so drift fails the build.
func simDiskKey(pin string) []byte {
	const simSalt = "localghost/sim-disk/v1"
	h := sha256.Sum256([]byte("localghost/pin/" + pin))
	return argon2.IDKey(h[:], []byte(simSalt), 1, 64*1024, 4, 32)
}
