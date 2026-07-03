package hw

// Sealer is the seal/unseal contract shared by the hardware (TPM) and software (PIN-derived) tiers.
// Setup picks one at provision time and records the choice in seal.env; the daemon reads that choice
// and constructs the matching Sealer at unlock. The two tiers are NOT build-tagged against each
// other , the software tier is always compiled in, and only the TPM implementation sits behind
// //go:build tpm (it needs the go-tpm dependency and a device). A box therefore ships knowing how to
// do both, and migrate-to-tpm / migrate-to-software just re-wrap the SAME AMK under the other tier.
//
// The AMK (the real LUKS key) is constant for the life of a container. A PIN change re-wraps it
// (ReKey), never re-encrypts the disk. Destroy makes the slot's AMK unrecoverable (crypto-erase).
type Sealer interface {
	// Seal wraps amk under pin for this slot and persists the wrapping. Overwrites any existing
	// wrapping for the slot (used at provision and, via ReKey, at PIN change).
	Seal(pin string, amk []byte) error
	// Unseal recovers the AMK for this slot given the PIN. A wrong PIN returns ErrWrongPIN, which is
	// the online rate-limit trigger; any other error is a genuine fault.
	Unseal(pin string) ([]byte, error)
	// ReKey re-wraps the existing AMK from oldPin to newPin without changing the AMK. Default
	// implementation is unseal-then-seal; the TPM tier overrides only if it has a cheaper path.
	ReKey(oldPin, newPin string) error
	// Destroy removes the slot's wrapping, making its AMK unrecoverable.
	Destroy() error
}

// ErrWrongPIN is returned by Unseal (and ReKey via Unseal) when the PIN does not authorize. It is
// deliberately the SAME error regardless of tier so callers , and the appears-down response , cannot
// distinguish "wrong PIN on a software box" from "wrong PIN on a TPM box".
var ErrWrongPIN = errSentinel("wrong pin")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// Seal mode strings persisted in seal.env (GHOST_SEAL_MODE).
const (
	SealModeTPM      = "tpm"
	SealModeSoftware = "software"
)
