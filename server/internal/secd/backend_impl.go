package secd

// The single unlock backend. One binary compiles both seal tiers (see internal/hw), and the tier is
// chosen at RUNTIME from the mode recorded in seal.env , not a build tag. A user can add or clear a
// TPM, or run migrate-to-tpm, without recompiling. The mounter/DataStore/Accounts/wiper wiring is
// identical for both tiers; the ONLY per-tier differences are how a slot's key is unsealed (the
// Sealer chosen by hw.SelectSealer) and what crypto-erase destroys (the sealer's Destroy).
//
// Not validated in CI (no TPM, no root, no encrypted volumes in the build env). Built against the
// documented go-tpm + cryptsetup interfaces and exercised on the box.

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/LocalGhostDao/localghost/server/internal/auth"
	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"github.com/LocalGhostDao/localghost/server/internal/profile"
	"github.com/LocalGhostDao/localghost/server/internal/wipe"
)

const tpmDevice = "/dev/tpmrm0"

type backend struct {
	accounts *profile.Accounts
	mounter  *hw.DMCryptMounter
	store    *hw.DataStore
	sealerAt func(slot int) (hw.Sealer, error) // resolves the slot's Sealer per the provisioned tier
	stateDir string
	sup      *Supervisor // supervises the ghost.*d daemons while mounted; nil until StartCache
}

func newDefaultBackend(cfg Config) UnlockBackend {
	sealStore := hw.NewEnvSealStore(filepath.Join(cfg.StateDir, "seal.env"))

	// mode is read fresh for each sealer construction, so a migrate (which rewrites seal.env) is
	// picked up without restarting the daemon. On an unprovisioned box mode is "" and SelectSealer
	// errors , resolution then rejects every PIN, which is the correct pre-provision behaviour.
	sealerAt := func(slot int) (hw.Sealer, error) {
		mode, _ := sealStore.Mode()
		return hw.SelectSealer(mode, tpmDevice, sealStore, slot)
	}

	// keyFor bridges the mounter to whichever tier is active. A tier/probe error (e.g. tpm mode but
	// no usable TPM) surfaces here as an unseal failure , the unlock fails cleanly and the reason is
	// logged, rather than silently downgrading.
	keyFor := func(slot int, pin string) ([]byte, error) {
		s, err := sealerAt(slot)
		if err != nil {
			return nil, err
		}
		return s.Unseal(pin)
	}
	mounter := hw.NewDMCryptMounter(cfg.StateDir, cfg.Disk, keyFor)
	store := hw.NewDataStore(func(slot int) string { return mounter.MountPath(slot) })

	reg, err := loadRegistry(cfg.StateDir)
	if err != nil {
		reg, _ = profile.NewRegistry(nil)
	}
	gate := auth.NewGate(auth.DefaultPolicy(), auth.NewMemoryStore())
	wiper := wipe.NewWiper(wipe.NewKeyVault(), sealerEraser{sealerAt}, func(slot int) error {
		return mounter.Unmount(slot)
	})
	accounts := profile.NewAccounts(reg, gate, wiper)

	return &backend{
		accounts: accounts, mounter: mounter, store: store,
		sealerAt: sealerAt, stateDir: cfg.StateDir,
	}
}

func (b *backend) Resolve(pin string) (int, bool, error) {
	d := b.accounts.Unlock("box", pin)
	switch d.Outcome {
	case profile.Open:
		return d.OpenSlot, d.Wiped, nil
	default:
		return profile.NoSlot, d.Wiped, nil
	}
}

func (b *backend) Unseal(slot int, pin string) ([]byte, error) {
	s, err := b.sealerAt(slot)
	if err != nil {
		return nil, err
	}
	return s.Unseal(pin)
}

func (b *backend) Mount(slot int, key []byte) error {
	if _, err := b.mounter.MapWithKey(slot, key); err != nil {
		return err
	}
	return b.mounter.ResizeToFill(slot)
}

func (b *backend) StartDB(slot int) error {
	_, err := b.store.Start(slot)
	return err
}

// StartCache is the last cold-unlock stage before DAEMONS. DataStore.Start already brought up Redis
// (StartDB), so there is no cache work here; instead this is where the supervisor starts the ghost.*d
// daemons, AFTER Postgres + Redis are up (they depend on both). On the warm path this is Skipped by
// runUnlock, so the daemons from the original cold unlock keep running , exactly right.
func (b *backend) StartCache(slot int) error {
	daemons := supervisedDaemons(b.mounter.MountPath(slot))
	sup := NewSupervisor()
	for name, port := range daemons {
		port := port
		sup.Register(SupervisedService{
			Name:       name,
			Critical:   criticalServices[name],
			HealthPort: port,
			Start:      spawnDaemon(filepath.Join(daemonBinDir, name), port),
			Stop:       func() error { return nil }, // SIGTERM via killProc handles shutdown
		})
	}
	// A critical daemon failing to start returns an error; the unlock stage marks Errored and the box
	// serves with that capability erroring , it does NOT block the mount or lock the box.
	if err := sup.Start(context.Background()); err != nil {
		b.sup = sup // keep it so Lock can still tear down whatever DID start
		return fmt.Errorf("supervisor: %w", err)
	}
	b.sup = sup
	return nil
}

// Supervisor exposes the running supervisor for /v1/status (nil when cold).
func (b *backend) SupervisorStatus() []ServiceStatus {
	if b.sup == nil {
		return nil
	}
	return b.sup.Status()
}

func (b *backend) Warm(slot int) bool { return b.mounter.IsMounted(slot) }

func (b *backend) Lock(slot int, emit func(profile.Progress)) error {
	step := func(st profile.Stage, do func() error) error {
		emit(profile.Progress{Stage: st, State: profile.Running})
		err := do()
		if err != nil {
			emit(profile.Progress{Stage: st, State: profile.Errored})
		} else {
			emit(profile.Progress{Stage: st, State: profile.Complete})
		}
		return err
	}
	// Stop the ghost.*d daemons FIRST and confirm every one is dead before we touch the volume. This
	// is the anti-wedge ordering: a daemon still holding the mount open would block Unmount. Teardown
	// returns only after every supervised process is reaped.
	_ = step(profile.StageStopServices, func() error {
		if b.sup == nil {
			return nil
		}
		err := b.sup.Teardown()
		b.sup = nil
		return err
	})
	_ = step(profile.StageStopCache, func() error { return b.store.StopCache(slot) })
	_ = step(profile.StageStopDB, func() error { return b.store.StopDB(slot) })
	if err := step(profile.StageUnmount, func() error { return b.mounter.Unmount(slot) }); err != nil {
		return err
	}
	emit(profile.Progress{Stage: profile.StageLocked, State: profile.Complete})
	return nil
}

// sealerEraser adapts whichever tier's Sealer to wipe.HardwareEraser: crypto-erase destroys a slot's
// sealed key (TPM object evicted, or software wrapping deleted from seal.env) so the volume's key can
// no longer be recovered.
type sealerEraser struct {
	sealerAt func(slot int) (hw.Sealer, error)
}

func (e sealerEraser) EraseAccount(slot int) error {
	s, err := e.sealerAt(slot)
	if err != nil {
		return err
	}
	return s.Destroy()
}
func (e sealerEraser) EraseAll() error {
	for _, slot := range []int{profile.MainSlot} {
		s, err := e.sealerAt(slot)
		if err != nil {
			return err
		}
		if err := s.Destroy(); err != nil {
			return err
		}
	}
	return nil
}
