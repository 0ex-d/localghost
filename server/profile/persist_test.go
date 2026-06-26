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
	if err := s.AddDecoy("2222", false); err != nil { // plain decoy
		t.Fatal(err)
	}
	if err := s.AddDecoy("3333", true); err != nil { // duress decoy, wipes main
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
		{"2222", true, 1},
		{"3333", true, 2},
		{"9999", false, NoSlot},
	} {
		r := loaded.Resolve(tc.pin)
		if r.Valid != tc.wantValid || (tc.wantValid && r.Open != tc.wantOpen) {
			t.Fatalf("pin %s: got valid=%v open=%d, want valid=%v open=%d",
				tc.pin, r.Valid, r.Open, tc.wantValid, tc.wantOpen)
		}
	}
	// The duress PIN must still carry its wipe-main behaviour.
	if r := loaded.Resolve("3333"); r.Wipe != MainSlot {
		t.Fatalf("duress PIN lost its wipe target after round-trip: %d", r.Wipe)
	}
}

func TestRegistryFileIsFixedSizeRegardlessOfRealCount(t *testing.T) {
	salt := []byte("box-salt-1234567")
	size := func(realPins int) int64 {
		s, _ := NewSetup(salt)
		_ = s.SetMain("0000")
		for i := 1; i < realPins; i++ {
			_ = s.AddDecoy(string(rune('1'+i)), false)
		}
		reg, _ := s.Finalize()
		p := filepath.Join(t.TempDir(), "r.blob")
		_ = reg.Save(p)
		fi, _ := os.Stat(p)
		return fi.Size()
	}
	// The on-disk size must not reveal how many real PINs exist (count-hiding deniability).
	if size(1) != size(3) {
		t.Fatal("registry file size leaks the real PIN count")
	}
}
