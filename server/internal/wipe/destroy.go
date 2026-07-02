package wipe

import "errors"

// Wiper performs the forensically-robust account crypto-erase triggered by the wipe PIN. It
// destroys ONE account's key, so the main account's data dies while every other PIN keeps opening
// its profile and nothing looks different. A global panic wipe is also available.
//
// The load-bearing step is the hardware eraser. Overwriting flash does not reliably destroy data
// (wear levelling, over-provisioning), so we do not rely on it. We destroy the account's master key
// in hardware that can truly erase its own cells, which makes that account's persisting ciphertext
// undecryptable. The disk scrub is belt-and-braces.
type Wiper struct {
	vault    *KeyVault
	hardware HardwareEraser
	scrub    func(slot int) error // best-effort overwrite+delete of a slot's wrapped key blob
}

func NewWiper(vault *KeyVault, hw HardwareEraser, scrub func(slot int) error) *Wiper {
	return &Wiper{vault: vault, hardware: hw, scrub: scrub}
}

// WipeAccount crypto-erases one slot. Idempotent: re-running on an already-wiped slot is a no-op, so
// a re-entry of the wipe PIN does nothing suspicious. It keeps going on individual errors
// (a wipe must be aggressive) and reports the combined result. After the hardware step the account
// is cryptographically gone.
func (w *Wiper) WipeAccount(slot int) error {
	var errs []error

	// 1. Memory: zeroise this slot's AMK now. A live attacker loses it immediately.
	if w.vault != nil {
		w.vault.wipeAccount(slot)
	}

	// 2. Hardware: evict this slot's sealed AMK so it cannot be reloaded after reboot. This is the
	//    crypto-erase. The slot's wrapped data key on disk is now permanently undecryptable.
	if w.hardware != nil {
		if err := w.hardware.EraseAccount(slot); err != nil {
			errs = append(errs, err)
		}
	} else {
		errs = append(errs, ErrNoHardwareErase)
	}

	// 3. Disk: best-effort scrub of this slot's wrapped key. Not relied upon, but cheap.
	if w.scrub != nil {
		if err := w.scrub(slot); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// PanicWipe destroys everything at once: the global crypto-erase the wipe PIN triggers. Use for
// "burn it all".
func (w *Wiper) PanicWipe(slots []int) error {
	var errs []error
	for _, s := range slots {
		if err := w.WipeAccount(s); err != nil {
			errs = append(errs, err)
		}
	}
	if w.hardware != nil {
		if err := w.hardware.EraseAll(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// HardwareEraser destroys account keys in hardware. Implementations:
//   - TPM: evict the sealed AMK object for a slot (EraseAccount), or clear the storage hierarchy
//     (EraseAll). The TPM erases its own NV, so the key is truly gone, not just dereferenced.
//   - Self-encrypting drive: a per-account key model maps poorly to a single drive media key, so on
//     an SED, EraseAll via crypto-erase (NVMe Format SES=2, ATA Secure Erase, TCG Opal) is the
//     global option; per-account erase stays TPM-based.
//
// This is the seam; the concrete TPM implementation is the next milestone and needs the hardware to
// test. Wiring it is what turns the wipe from strong into forensically robust.
type HardwareEraser interface {
	EraseAccount(slot int) error
	EraseAll() error
}

// ErrNoHardwareErase signals no hardware anchor was available, so the wipe fell back to memory
// zeroise plus best-effort scrub. The daemon MUST surface this: without the hardware step a forensic
// attacker with a prior disk image and the eventual PIN could still recover that account.
var ErrNoHardwareErase = errors.New("no hardware key-erase available; wipe is best-effort only")
