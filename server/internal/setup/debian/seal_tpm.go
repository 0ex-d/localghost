//go:build tpm

package debian

import (
	"crypto/rand"
	"fmt"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// sealAndFormat (real TPM build): generate a random AMK, seal it in the TPM bound to the main PIN,
// then LUKS-format the raw disk with that AMK. The AMK only exists in memory long enough to seal and
// format, then is zeroised. Unlock later calls hw.TPMSealedKey.Unseal(pin); a wrong PIN trips the
// TPM's hardware dictionary-attack lockout, which is what makes a short PIN safe and an offline disk
// attack useless.
func (s *System) sealAndFormat() error {
	// Sole-tenant check FIRST, before any destructive work: the DA lockout we set below is global to
	// the TPM, so if another tenant already has persistent objects we ask the operator before going
	// on. Aborting here costs nothing; aborting after the LUKS format would have wiped the disk.
	if err := s.checkTPMSoleTenant(); err != nil {
		return err
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
		// best-effort: if formatting failed, drop the sealed key so we do not leave a sealed AMK with
		// no container behind it.
		_ = sealed.Evict()
		return err
	}

	// Finally, set the DA lockout: bind the lockout hierarchy to pinAuth(PIN) and apply the policy
	// (5 tries, 24h recoveries). This is the real brute-force wall for the first-unlock path. Done
	// LAST so an earlier failure never leaves a global policy set on a box we then abort.
	if err := hw.SetupLockout(s.TPMDevice, s.MainPIN); err != nil {
		_ = sealed.Evict()
		return fmt.Errorf("set TPM dictionary-attack lockout: %w", err)
	}
	return nil
}

// checkTPMSoleTenant enumerates persistent TPM objects and, if any are not ours, asks the operator
// whether to proceed. Fails closed if no Confirm callback is wired (an unresolved shared-TPM
// question must not silently reconfigure a global lockout).
func (s *System) checkTPMSoleTenant() error {
	foreign, err := hw.ForeignPersistentHandles(s.TPMDevice)
	if err != nil {
		return fmt.Errorf("check TPM tenancy: %w", err)
	}
	if len(foreign) == 0 {
		return nil // sole tenant, safe to set a global DA policy
	}
	list := ""
	for _, h := range foreign {
		list += fmt.Sprintf(" 0x%08X", h)
	}
	prompt := fmt.Sprintf(
		"This TPM already holds objects LocalGhost did not create:%s\n"+
			"LocalGhost sets a GLOBAL dictionary-attack lockout (5 tries) that will apply to those too,\n"+
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
