package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistrySaveLoadRoundTrip(t *testing.T) {
	salt := []byte("box-salt-1234567")
	s, err := NewSetup(salt)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMain("1111"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWipe("3333"); err != nil { // wipe PIN, crypto-erases everything
		t.Fatal(err)
	}
	reg, err := s.Finalize()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "registry.blob")
	if err := reg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Every PIN must resolve identically after a round-trip.
	for _, tc := range []struct {
		pin       string
		wantValid bool
		wantOpen  int
	}{
		{"1111", true, MainSlot},
		{"3333", true, NoSlot}, // wipe PIN opens nothing
		{"9999", false, NoSlot},
	} {
		r := loaded.Resolve(tc.pin)
		if r.Valid != tc.wantValid || (tc.wantValid && r.Open != tc.wantOpen) {
			t.Fatalf("pin %s: got valid=%v open=%d, want valid=%v open=%d",
				tc.pin, r.Valid, r.Open, tc.wantValid, tc.wantOpen)
		}
	}
	// The wipe PIN must still carry its WipeAll behaviour after a round-trip.
	if r := loaded.Resolve("3333"); r.Wipe != WipeAll {
		t.Fatalf("wipe PIN lost its WipeAll action after round-trip: %d", r.Wipe)
	}
}

func TestRegistryFileIsFixedSizeRegardlessOfRealCount(t *testing.T) {
	salt := []byte("box-salt-1234567")
	// size(withWipe) builds a registry with a main PIN, optionally a wipe PIN, saves it, returns
	// the file size. The on-disk size must be identical whether or not a wipe PIN exists, so the
	// stored form never reveals that a wipe PIN is present.
	size := func(withWipe bool) int64 {
		s, _ := NewSetup(salt)
		_ = s.SetMain("0000")
		if withWipe {
			_ = s.SetWipe("9999")
		}
		reg, _ := s.Finalize()
		p := filepath.Join(t.TempDir(), "r.blob")
		_ = reg.Save(p)
		fi, _ := os.Stat(p)
		return fi.Size()
	}
	if size(false) != size(true) {
		t.Fatal("registry file size leaks whether a wipe PIN exists")
	}
}
