package debian

// sealAndFormat provisions the encrypted store in whichever tier the operator selected. One code
// path, tier chosen at RUNTIME from s.SealMode (default "tpm"), no build tags. Both tiers generate a
// random AMK, format the raw LUKS disk with it, and record the tier in seal.env; they differ only in
// how the AMK is wrapped:
//
//   - tpm      : sealed in the TPM under the PIN; seal.env stores ONLY the mode. Adds a global
//                dictionary-attack lockout (the hardware brute-force wall). Requires a usable TPM.
//   - software : wrapped with ChaCha20-Poly1305 under an Argon2id KEK; seal.env stores the salt +
//                wrapped AMK. No hardware lockout , offline-brute-forceable with a stolen disk and a
//                weak PIN. For machines without a TPM.

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

func (s *System) sealAndFormat() error {
	mode := s.SealMode
	if mode == "" {
		mode = hw.SealModeTPM // default: hardware tier
	}
	store := hw.NewEnvSealStore(filepath.Join(s.StateDir, "seal.env"))

	switch mode {
	case hw.SealModeTPM:
		return s.sealTPM(store)
	case hw.SealModeSoftware:
		return s.sealSoftware(store)
	default:
		return fmt.Errorf("unknown --seal mode %q (want tpm or software)", mode)
	}
}

func (s *System) sealTPM(store *hw.EnvSealStore) error {
	// Fail early and clearly if the operator asked for the TPM tier on a box with no usable TPM,
	// rather than part-provisioning and failing at the seal call. This is the secure-by-default
	// stance: --seal defaults to tpm, and if the TPM is not there we STOP (the operator can pass
	// --seal software deliberately), never silently downgrade.
	if !hw.TPMUsable(s.TPMDevice) {
		return fmt.Errorf("--seal tpm (the default) but %s is not usable (missing, no permission, "+
			"or firmware-wedged). Fix TPM access, or pass --seal software to provision the "+
			"PIN-derived tier deliberately", s.TPMDevice)
	}

	// Sole-tenant check FIRST, before destructive work: the DA lockout is global to the TPM.
	if err := s.checkTPMSoleTenant(); err != nil {
		return err
	}
	if err := store.SetMode(hw.SealModeTPM); err != nil {
		return fmt.Errorf("record seal mode: %w", err)
	}

	amk := make([]byte, 32)
	if _, err := rand.Read(amk); err != nil {
		return fmt.Errorf("generate AMK: %w", err)
	}
	defer zeroBytes(amk)

	sealed := hw.NewTPMSealedKey(s.TPMDevice, ghostSlot)
	if err := sealed.Reseal(s.MainPIN, amk); err != nil {
		return fmt.Errorf("seal AMK in TPM: %w", err)
	}
	if err := s.formatLUKS(amk); err != nil {
		_ = sealed.Evict()
		return err
	}
	// DA lockout LAST, so an earlier failure never leaves a global policy on a box we then abort.
	if err := hw.SetupLockout(s.TPMDevice, s.MainPIN); err != nil {
		_ = sealed.Evict()
		return fmt.Errorf("set TPM dictionary-attack lockout: %w", err)
	}
	return nil
}

func (s *System) sealSoftware(store *hw.EnvSealStore) error {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Provisioning with the SOFTWARE seal tier (no TPM).")
	fmt.Fprintln(os.Stderr, "  The disk is encrypted, but the key is derived from the PIN: an attacker")
	fmt.Fprintln(os.Stderr, "  with the raw disk can brute-force a weak PIN offline. Use a strong PIN,")
	fmt.Fprintln(os.Stderr, "  and migrate to the TPM tier once hardware is available.")
	fmt.Fprintln(os.Stderr, "")

	if err := store.SetMode(hw.SealModeSoftware); err != nil {
		return fmt.Errorf("record seal mode: %w", err)
	}
	amk, err := hw.GenerateAMK()
	if err != nil {
		return err
	}
	defer zeroBytes(amk)

	// Seal FIRST (writes salt + wrapped AMK), then format with the same AMK; on format failure drop
	// the wrapping so no half-state remains.
	sealer := hw.NewSoftwareSealer(store, ghostSlot)
	if err := sealer.Seal(s.MainPIN, amk); err != nil {
		return fmt.Errorf("seal AMK: %w", err)
	}
	if err := s.formatLUKS(amk); err != nil {
		_ = sealer.Destroy()
		return err
	}
	return nil
}

// checkTPMSoleTenant enumerates persistent TPM objects and, if any are not ours, asks the operator
// whether to proceed. Fails closed if no Confirm callback is wired.
func (s *System) checkTPMSoleTenant() error {
	foreign, err := hw.ForeignPersistentHandles(s.TPMDevice)
	if err != nil {
		return fmt.Errorf("check TPM tenancy: %w", err)
	}
	if len(foreign) == 0 {
		return nil
	}
	list := ""
	for _, h := range foreign {
		list += fmt.Sprintf(" 0x%08X", h)
	}
	prompt := fmt.Sprintf(
		"This TPM already holds objects LocalGhost did not create:%s\n"+
			"LocalGhost sets a GLOBAL dictionary-attack lockout that will apply to those too,\n"+
			"and a future `resetup` runs `tpm2 clear`, which DESTROYS them. Proceed anyway? [y/N]: ",
		list)
	if s.Confirm == nil {
		return fmt.Errorf("TPM is not sole-tenant (%s) and no confirmation prompt is available; aborting", list)
	}
	ok, err := s.Confirm(prompt)
	if err != nil {
		return fmt.Errorf("tenancy confirmation: %w", err)
	}
	if !ok {
		return fmt.Errorf("aborted: TPM has other tenants (%s) and operator declined", list)
	}
	return nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
