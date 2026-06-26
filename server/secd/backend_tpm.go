//go:build tpm

package secd

import (
	"errors"
	"path/filepath"

	"github.com/LocalGhostDao/localghost/server/auth"
	"github.com/LocalGhostDao/localghost/server/hw"
	"github.com/LocalGhostDao/localghost/server/profile"
	"github.com/LocalGhostDao/localghost/server/wipe"
)

// This is the REAL unlock backend, compiled with `-tags tpm` on the box. It wires:
//   - profile.Accounts   PIN resolution (real / decoy / duress-wipe / reject), rate-limited
//   - hw.TPMSealedKey     per-slot key seal/unseal against /dev/tpmrm0 (Intel PTT on xyntai)
//   - hw.DMCryptMounter   LUKS map + filesystem mount of the slot's container
//   - hw.DataStore        per-account Postgres + Redis inside the mounted volume
//
// None of this is validated in CI (no TPM, no root, no encrypted volumes in the build env). It is
// built against the documented go-tpm and cryptsetup interfaces and MUST be exercised on the box:
//   go build -tags tpm ./...        # needs go-tpm in the module
//   sudo ghost.secd ...             # needs /dev/tpmrm0, cryptsetup, postgres, redis
//
// Build error here is expected without `go get github.com/google/go-tpm`; that dependency is only
// pulled in for the tpm build, so the default build (and its tests) stay dependency-light.

const tpmDevice = "/dev/tpmrm0"

type tpmBackend struct {
	accounts *profile.Accounts
	mounter  *hw.DMCryptMounter
	store    *hw.DataStore
	stateDir string
}

func newDefaultBackend(cfg Config) UnlockBackend {
	// keyFor lets the mounter ask the TPM to unseal a slot's key with the supplied PIN.
	keyFor := func(slot int, pin string) ([]byte, error) {
		return hw.NewTPMSealedKey(tpmDevice, slot).Unseal(pin)
	}
	mounter := hw.NewDMCryptMounter(cfg.StateDir, keyFor)
	store := hw.NewDataStore(func(slot int) string { return mounter.MountPath(slot) })

	// The account registry + rate-limit gate + wiper. The registry is loaded from the box's
	// enrollment state; the wiper destroys a slot's TPM-sealed key on crypto-erase.
	reg, err := loadRegistry(cfg.StateDir)
	if err != nil {
		// No registry yet (pre-enrolment) is not fatal for bringing the daemon up; resolution will
		// simply reject every PIN until setup writes one.
		reg, _ = profile.NewRegistry(nil)
	}
	gate := auth.NewGate(auth.DefaultPolicy(), auth.NewMemoryStore())
	// The wiper destroys a slot's TPM-sealed key (hardware crypto-erase) and scrubs the mount.
	wiper := wipe.NewWiper(wipe.NewKeyVault(), tpmEraser{}, func(slot int) error {
		return mounter.Unmount(slot)
	})
	accounts := profile.NewAccounts(reg, gate, wiper)

	return &tpmBackend{accounts: accounts, mounter: mounter, store: store, stateDir: cfg.StateDir}
}

func (b *tpmBackend) Resolve(pin string) (int, bool, error) {
	// "device" id for the rate limiter is the box itself here; the real per-device id comes from the
	// client cert at the HTTP layer. Resolve performs any duress crypto-erase internally.
	d := b.accounts.Unlock("box", pin)
	switch d.Outcome {
	case profile.Open:
		return d.OpenSlot, d.MainWiped, nil
	default:
		return profile.NoSlot, d.MainWiped, nil
	}
}

func (b *tpmBackend) Unseal(slot int, pin string) ([]byte, error) {
	return hw.NewTPMSealedKey(tpmDevice, slot).Unseal(pin)
}

func (b *tpmBackend) Mount(slot int, key []byte) error {
	// Map the LUKS volume with the key the Unseal stage already produced (no second unseal), then
	// grow the filesystem to fill the equal-size container so a decoy looks full to its owner.
	if _, err := b.mounter.MapWithKey(slot, key); err != nil {
		return err
	}
	return b.mounter.ResizeToFill(slot)
}

func (b *tpmBackend) StartDB(slot int) error {
	_, err := b.store.Start(slot) // Start brings up both; StartCache is a no-op follow-through
	return err
}

func (b *tpmBackend) StartCache(slot int) error {
	// DataStore.Start brought up Redis too; nothing more to do. Kept as a stage for the UI.
	return nil
}

func (b *tpmBackend) Warm(slot int) bool {
	return b.mounter.IsMounted(slot)
}

// tpmEraser adapts the TPM to wipe.HardwareEraser: crypto-erase destroys a slot's sealed key so the
// volume becomes unrecoverable.
type tpmEraser struct{}

func (tpmEraser) EraseAccount(slot int) error {
	return hw.NewTPMSealedKey(tpmDevice, slot).Evict()
}
func (tpmEraser) EraseAll() error {
	for slot := 0; slot < profile.TotalSlots; slot++ {
		if err := hw.NewTPMSealedKey(tpmDevice, slot).Evict(); err != nil {
			return err
		}
	}
	return nil
}

var errReject = errors.New("unlock rejected")

// loadRegistry reads the account registry from the box state dir. The registry blob is written at
// setup/enrolment; this loads it for the running daemon.
func loadRegistry(stateDir string) (*profile.Registry, error) {
	return profile.LoadRegistry(filepath.Join(stateDir, "registry.blob"))
}
