//go:build !tpm

package secd

import (
	"crypto/sha256"
	"testing"

	"golang.org/x/crypto/argon2"
)

// TestSimDiskKeyMatchesSetup pins the daemon's simDiskKey to the exact derivation the setup path
// (debian.SimDiskKey) uses. They are duplicated across packages on purpose (the daemon must not link
// provisioning deps), so this test is the tripwire: if one derivation changes and the other does not,
// a --insecure-sim container would stop opening, and this fails first.
func TestSimDiskKeyMatchesSetup(t *testing.T) {
	pin := "correct horse battery staple"
	h := sha256.Sum256([]byte("localghost/pin/" + pin))
	want := argon2.IDKey(h[:], []byte("localghost/sim-disk/v1"), 1, 64*1024, 4, 32)
	got := simDiskKey(pin)
	if len(got) != 32 {
		t.Fatalf("key length %d, want 32", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("simDiskKey diverged from the setup derivation at byte %d", i)
		}
	}
}
