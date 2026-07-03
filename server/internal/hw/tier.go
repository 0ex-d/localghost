package hw

// Runtime seal-tier selection , replaces the //go:build tpm split. One binary compiles BOTH tiers
// (go-tpm is pure Go and never opens a device at init, so it links harmlessly on a TPM-less box) and
// picks the tier at runtime. The rule that matters, and the reason this is not "auto-detect": we NEVER
// silently downgrade. A box provisioned with the TPM tier sealed its AMK in hardware; the software
// tier cannot unseal it (different key custody). So if seal.env says tpm and the TPM is unusable, the
// honest outcome is a hard stop , the box is locked until the TPM returns , not a silent fall to a
// software backend that would fail to unseal anyway, more confusingly.

import (
	"fmt"

	"github.com/google/go-tpm/tpm2/transport"
)

// TPMUsable reports whether the TPM device can be opened. It does NOT probe lockout state (that needs
// an auth attempt, which would spend the DA budget); it only answers "is there a TPM we can talk to".
// A usable-but-locked TPM still returns true here , the unseal attempt is where lockout surfaces,
// and surfacing it there (as a failed unlock) is correct, not at selection time.
func TPMUsable(device string) bool {
	tpm, err := transport.OpenTPM(device)
	if err != nil {
		return false
	}
	_ = tpm.Close()
	return true
}

// SelectSealer builds the Sealer for a slot according to the provisioned mode. Callers pass the mode
// read from seal.env (SealModeTPM / SealModeSoftware) and the stores/paths each tier needs.
//
//   - mode software: SoftwareSealer over the SealStore. No TPM touched.
//   - mode tpm: TPMSealedKey, but ONLY if the device opens. If not, a descriptive error , the caller
//     must surface it, not fall back.
//   - mode "" (unprovisioned): error , selection is meaningless before provisioning.
func SelectSealer(mode, tpmDevice string, store SealStore, slot int) (Sealer, error) {
	switch mode {
	case SealModeSoftware:
		return NewSoftwareSealer(store, slot), nil
	case SealModeTPM:
		if !TPMUsable(tpmDevice) {
			return nil, fmt.Errorf(
				"box was provisioned with the TPM seal tier but %s is not usable (missing, no "+
					"permission, or firmware-wedged). The disk key is sealed in hardware and cannot be "+
					"recovered without the TPM , this is not a fall-back-to-software situation. Fix TPM "+
					"access (group/udev, or a firmware clear) and retry; the data is intact, just locked",
				tpmDevice)
		}
		return NewTPMSealedKey(tpmDevice, slot), nil
	case "":
		return nil, fmt.Errorf("box is not provisioned (no seal mode recorded); run ghost-setup")
	default:
		return nil, fmt.Errorf("unknown seal mode %q in seal.env", mode)
	}
}
