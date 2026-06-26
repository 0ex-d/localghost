package profile

import "golang.org/x/crypto/argon2"

// PinKey derives a key from a PIN with a deliberately costly KDF. boxSalt personalises it to this
// box (not secret; it stops cross-box precomputation). The cost is the per-attempt price, which
// also slows brute force. The result is used two ways: as the registry identifier hash, and as the
// wrapping key that (together with the TPM-held master key) unwraps a profile's data key.
const (
	argonTime    = 4
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	keyLen       = 32
)

func PinKey(pin string, boxSalt []byte) []byte {
	return argon2.IDKey([]byte(pin), boxSalt, argonTime, argonMemory, argonThreads, keyLen)
}
