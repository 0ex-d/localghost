package hw

// Software seal tier: for machines without a TPM. A random 256-bit AMK is the real LUKS key; it is
// wrapped with ChaCha20-Poly1305 under a key-encryption-key derived from the PIN via Argon2id, and
// the wrapped blob plus a per-box salt live in seal.env. The disk is genuinely encrypted at rest.
//
// The honest limit, stated where the code is rather than only in a doc: this tier's entire strength
// is Argon2id-cost x PIN-entropy. An attacker with the raw disk also has seal.env (it sits on the
// unencrypted boot volume , there is nowhere hardware-safe to hide it without a TPM), so they can
// brute-force the PIN offline at their hardware's rate, with NO lockout , the TPM tier's hardware DA
// counter has no software equivalent. A short PIN on a stolen disk is game over here. This tier
// resists online guessing (ghost.secd rate-limits, and a wrong PIN fails at the Poly1305 tag) and
// casual offline attempts; it does not resist a determined offline attacker. That is exactly why the
// TPM tier exists, and why --seal defaults to tpm.

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// Argon2id cost. Tuned HARD because it is the only thing between a stolen disk and the AMK: t=3
// passes, 256MiB memory, p=4 lanes. ~0.5-1s per guess on a modern CPU, and memory-hardness blunts
// GPU/ASIC parallelism , the point is to make an offline PIN search expensive, not instant.
const (
	argonTime    = 3
	argonMemory  = 256 * 1024 // KiB => 256 MiB
	argonThreads = 4
	argonKeyLen  = 32
	amkLen       = 32
	saltLen      = 16
)

// SealStore is what the software tier reads and writes: the per-box salt and per-slot wrapped AMKs,
// persisted by the caller (setup writes seal.env; the daemon reads it). Kept as an interface so the
// crypto here is independent of the file format , see EnvSealStore for the seal.env implementation.
type SealStore interface {
	Salt() ([]byte, error)          // the per-box Argon2id salt; created once at first Seal
	SetSalt(salt []byte) error      // persist a freshly generated salt
	Wrapped(slot int) ([]byte, error)   // the wrapped AMK for a slot, or (nil, nil) if absent
	SetWrapped(slot int, blob []byte) error
	DeleteWrapped(slot int) error
}

// SoftwareSealer implements Sealer for one slot over a SealStore.
type SoftwareSealer struct {
	slot  int
	store SealStore
}

func NewSoftwareSealer(store SealStore, slot int) *SoftwareSealer {
	return &SoftwareSealer{slot: slot, store: store}
}

// GenerateAMK returns a fresh random AMK. Setup calls this once per container, seals it, and
// LUKS-formats with it. Exported so the setup path can format the disk with the same bytes it seals.
func GenerateAMK() ([]byte, error) {
	amk := make([]byte, amkLen)
	if _, err := rand.Read(amk); err != nil {
		return nil, fmt.Errorf("generate AMK: %w", err)
	}
	return amk, nil
}

// kek derives the key-encryption-key from pin+salt. Identical derivation is the whole contract:
// change it and every existing software box stops unlocking.
func kek(pin string, salt []byte) []byte {
	// Hash the PIN first so its length is uniform before Argon2id (matches the TPM tier's pinAuth
	// domain separation, keeping "the PIN as bytes" consistent across the codebase).
	h := sha256.Sum256([]byte("localghost/pin/" + pin))
	return argon2.IDKey(h[:], salt, argonTime, argonMemory, argonThreads, argonKeyLen)
}

func (s *SoftwareSealer) Seal(pin string, amk []byte) error {
	if len(amk) == 0 {
		return errors.New("refusing to seal an empty AMK")
	}
	salt, err := s.ensureSalt()
	if err != nil {
		return err
	}
	key := kek(pin, salt)
	defer zeroize(key)

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return fmt.Errorf("aead: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("nonce: %w", err)
	}
	// blob = nonce || ciphertext(+tag). Slot is bound as additional data so a slot-0 blob cannot be
	// pasted into slot 1 and still authenticate.
	ct := aead.Seal(nil, nonce, amk, s.aad())
	blob := append(nonce, ct...)
	return s.store.SetWrapped(s.slot, blob)
}

func (s *SoftwareSealer) Unseal(pin string) ([]byte, error) {
	salt, err := s.store.Salt()
	if err != nil || len(salt) == 0 {
		return nil, fmt.Errorf("no salt for this box (not provisioned in software mode?): %w", err)
	}
	blob, err := s.store.Wrapped(s.slot)
	if err != nil {
		return nil, err
	}
	if len(blob) == 0 {
		return nil, fmt.Errorf("no sealed key for slot %d", s.slot)
	}
	key := kek(pin, salt)
	defer zeroize(key)

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("aead: %w", err)
	}
	ns := aead.NonceSize()
	if len(blob) < ns {
		return nil, fmt.Errorf("wrapped blob too short for slot %d", s.slot)
	}
	nonce, ct := blob[:ns], blob[ns:]
	amk, err := aead.Open(nil, nonce, ct, s.aad())
	if err != nil {
		// Poly1305 tag mismatch: wrong PIN (or tampered blob). Same sentinel either way , callers
		// must not distinguish, and the daemon turns this into the appears-down response.
		return nil, ErrWrongPIN
	}
	return amk, nil
}

func (s *SoftwareSealer) ReKey(oldPin, newPin string) error {
	amk, err := s.Unseal(oldPin) // returns ErrWrongPIN on a bad oldPin
	if err != nil {
		return err
	}
	defer zeroize(amk)
	return s.Seal(newPin, amk) // same AMK, new KEK; disk untouched
}

func (s *SoftwareSealer) Destroy() error { return s.store.DeleteWrapped(s.slot) }

func (s *SoftwareSealer) ensureSalt() ([]byte, error) {
	salt, err := s.store.Salt()
	if err == nil && len(salt) == saltLen {
		return salt, nil
	}
	salt = make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	if err := s.store.SetSalt(salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// aad binds a wrapping to its slot so blobs are not interchangeable between slots.
func (s *SoftwareSealer) aad() []byte { return []byte(fmt.Sprintf("localghost/seal/slot/%d", s.slot)) }

func zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
