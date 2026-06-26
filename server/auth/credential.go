package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. A PIN is low entropy, so we lean on a deliberately costly KDF. These bound
// how fast even an offline attacker (box root, who can read the hash) can guess. They do NOT make
// a short PIN safe against root, they only buy time. Real protection against root is the TPM seam.
const (
	argonTime    = 4
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

// Credential is the stored verifier for a PIN. The PIN itself is never stored. salt and hash are
// safe to persist; an attacker who reads them still has to brute-force, which the Gate rate-limits
// online and which a strong KDF slows offline.
type Credential struct {
	Salt []byte
	Hash []byte
}

// NewCredential derives a verifier from a PIN. Call this at enrollment / PIN change.
func NewCredential(pin string) (Credential, error) {
	if pin == "" {
		return Credential{}, fmt.Errorf("empty PIN")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return Credential{}, err
	}
	hash := argon2.IDKey([]byte(pin), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return Credential{Salt: salt, Hash: hash}, nil
}

// Verify checks a PIN against the stored credential in constant time. Returns true on match. This
// is the expensive step; the Gate ensures it only runs when an attempt is actually allowed, so it
// cannot be used to spam the KDF as a DoS.
func (c Credential) Verify(pin string) bool {
	candidate := argon2.IDKey([]byte(pin), c.Salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return subtle.ConstantTimeCompare(candidate, c.Hash) == 1
}
