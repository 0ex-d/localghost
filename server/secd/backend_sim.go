//go:build !tpm

package secd

import (
	"errors"
	"time"

	"github.com/LocalGhostDao/localghost/server/profile"
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
func (simBackend) Unseal(int, string) ([]byte, error) { return make([]byte, 32), nil }
func (simBackend) Mount(int, []byte) error            { time.Sleep(400 * time.Millisecond); return nil }
func (simBackend) StartDB(int) error                  { time.Sleep(300 * time.Millisecond); return nil }
func (simBackend) StartCache(int) error               { time.Sleep(100 * time.Millisecond); return nil }
func (simBackend) Warm(int) bool                      { return false }
