package wipe

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"crypto/hkdf"
)

// KeyVault holds a separate account master key (AMK) per profile slot. Each slot's data is wrapped
// so unwrapping needs BOTH that slot's AMK AND the slot's PIN-derived key. Because the keys are
// per-slot, wiping one slot destroys only its AMK: the wipe PIN crypto-erases the account
// while every other PIN keeps opening its profile, so nothing looks different afterwards.
//
// Each AMK is random (never PIN-derived) and is sealed in hardware (see destroy.go). Destroying one
// AMK makes that slot's ciphertext permanently undecryptable, which survives flash wear-levelling
// and a prior disk image.
type KeyVault struct {
	amk map[int]*Secret // slot -> 32-byte account master key, mlock'd
}

func NewKeyVault() *KeyVault {
	return &KeyVault{amk: make(map[int]*Secret)}
}

// SetAccountKey installs (or loads from hardware) the AMK for a slot.
func (v *KeyVault) SetAccountKey(slot int, key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("account key must be 32 bytes")
	}
	s := NewSecret(32)
	copy(s.Bytes(), key)
	v.amk[slot] = s
	return nil
}

// NewAccountKey generates a fresh random AMK for a slot and returns it so the caller can seal it in
// hardware. Handle the returned bytes briefly.
func (v *KeyVault) NewAccountKey(slot int) ([]byte, error) {
	s := NewSecret(32)
	if _, err := io.ReadFull(rand.Reader, s.Bytes()); err != nil {
		return nil, err
	}
	v.amk[slot] = s
	return s.Bytes(), nil
}

// WrapDataKey seals a slot's random data key so it needs both the slot's AMK and the PIN key.
func (v *KeyVault) WrapDataKey(slot int, pinKey, dataKey []byte) ([]byte, error) {
	gcm, err := v.wrapper(slot, pinKey)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, dataKey, nil)...), nil
}

// UnwrapDataKey recovers a slot's data key. Fails if the slot's AMK has been wiped or the PIN is
// wrong. Failure after a wipe is the point: that account's data is gone.
func (v *KeyVault) UnwrapDataKey(slot int, pinKey, wrapped []byte) ([]byte, error) {
	gcm, err := v.wrapper(slot, pinKey)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(wrapped) < ns {
		return nil, fmt.Errorf("wrapped key too short")
	}
	return gcm.Open(nil, wrapped[:ns], wrapped[ns:], nil)
}

// wipeAccount zeroises one slot's AMK in memory. The hardware eviction is done by the Wiper.
func (v *KeyVault) wipeAccount(slot int) {
	if s, ok := v.amk[slot]; ok {
		s.Destroy()
		delete(v.amk, slot)
	}
}

func (v *KeyVault) wrapper(slot int, pinKey []byte) (cipher.AEAD, error) {
	s, ok := v.amk[slot]
	if !ok {
		return nil, fmt.Errorf("no account key for slot %d (wiped or not loaded)", slot)
	}
	secret := append(append([]byte{}, s.Bytes()...), pinKey...)
	wk, err := hkdf.Key(sha256.New, secret, nil, "localghost/wrap", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(wk)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
