package auth

import "errors"

// This file marks the boundary between what software can defend and what needs hardware.
//
// The Gate above defends against a phone attacker with NO box root: it rate-limits and locks out,
// and a phone holder cannot read the credential or reset the counter. Complete for that threat.
//
// Against an attacker with ROOT on the box, the Gate is bypassable: root reads the Credential and
// the AttemptStore and brute-forces offline, or patches the daemon. No Go code changes that. The
// only real defence is a hardware root of trust that (a) holds the master key non-extractably and
// (b) enforces its own dictionary-attack lockout that root cannot reset.
//
// SealedKey is that seam. A TPM 2.0 implementation seals the persona master key behind a PIN-bound
// auth policy. Unseal supplies the PIN as the object's auth value; the TPM checks it, increments
// its hardware DA counter on failure, and refuses after maxTries until recoveryTime elapses. Root
// cannot extract the key or reset the DA lockout without the lockout auth.
//
// Caveats, restated so they live next to the code:
//   - fTPM (likely Intel PTT on this box) is weaker than discrete and has had glitching/side
//     channel breaks. It raises the bar; it is not absolute against physical attack.
//   - In a guest VM this is a vTPM: safe against guest-root only if you trust the host.
//   - It stops brute force and key extraction. It does NOT stop active root from scraping the PIN
//     out of memory during a legitimate unlock. Reduce that window (mlock, zeroise, no swap/logs).
type SealedKey interface {
	// Unseal returns the master key if pin satisfies the TPM auth policy. The TPM enforces the
	// attempt lockout, so the software Gate is unnecessary in front of this (and would be weaker).
	Unseal(pin string) (key []byte, err error)

	// Reseal binds a (new) PIN to the key. Used at enrollment and PIN change.
	Reseal(pin string, key []byte) error
}

// ErrNoTPM is returned by a SealedKey factory when the box has no usable TPM 2.0. The daemon should
// surface this loudly: without it, the box-root threat is undefended and the operator should know.
var ErrNoTPM = errors.New("no usable TPM 2.0; master key cannot be hardware-sealed")

// OpenSealedKey is implemented in a build that links a TPM library (e.g. go-tpm). Kept as a stub
// here so the seam is explicit and the rest of ghost.secd compiles without the TPM backend during
// early development. Wiring this is the next security milestone after the Gate.
func OpenSealedKey() (SealedKey, error) {
	return nil, ErrNoTPM
}
