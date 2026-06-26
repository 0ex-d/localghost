package wipe

import "syscall"

// Secret is a key held in memory we intend to destroy on wipe. It is mlock'd so it never reaches
// swap (where a wipe could not reach it), and Destroy zeroises it explicitly.
//
// Go caveat, stated plainly: the garbage collector may copy heap memory, so a secret that passes
// through a Go string or an interface can leave copies we cannot reach. Keep secrets in a Secret
// (a fixed byte slice we own) from the moment they exist, never in strings, and zeroise promptly.
// This is best-effort hardening, not a guarantee against a kernel-level attacker reading live RAM.
type Secret struct {
	b      []byte
	locked bool
}

// NewSecret allocates an mlock'd buffer of n bytes. Fill it in place; do not copy it elsewhere.
func NewSecret(n int) *Secret {
	b := make([]byte, n)
	s := &Secret{b: b}
	if err := syscall.Mlock(b); err == nil {
		s.locked = true
	}
	return s
}

func (s *Secret) Bytes() []byte { return s.b }

// Destroy zeroises the buffer and unlocks it. Call this on wipe and on normal teardown.
func (s *Secret) Destroy() {
	for i := range s.b {
		s.b[i] = 0
	}
	if s.locked {
		_ = syscall.Munlock(s.b)
		s.locked = false
	}
}
