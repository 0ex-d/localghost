package hw

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestSoftwareSealRoundTrip(t *testing.T) {
	store := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	s := NewSoftwareSealer(store, 0)

	amk, err := GenerateAMK()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal("1234", amk); err != nil {
		t.Fatal(err)
	}
	got, err := s.Unseal("1234")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, amk) {
		t.Fatal("unseal did not recover the AMK")
	}
}

func TestSoftwareWrongPIN(t *testing.T) {
	store := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	s := NewSoftwareSealer(store, 0)
	amk, _ := GenerateAMK()
	_ = s.Seal("correct", amk)

	_, err := s.Unseal("wrong")
	if err != ErrWrongPIN {
		t.Fatalf("wrong PIN must return ErrWrongPIN, got %v", err)
	}
}

func TestSoftwareReKeyPreservesAMK(t *testing.T) {
	store := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	s := NewSoftwareSealer(store, 0)
	amk, _ := GenerateAMK()
	_ = s.Seal("old", amk)

	if err := s.ReKey("old", "new"); err != nil {
		t.Fatal(err)
	}
	// Old PIN no longer works; new PIN recovers the ORIGINAL amk (disk key unchanged).
	if _, err := s.Unseal("old"); err != ErrWrongPIN {
		t.Fatalf("old PIN should fail after ReKey, got %v", err)
	}
	got, err := s.Unseal("new")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, amk) {
		t.Fatal("ReKey changed the AMK , disk would be unreadable")
	}
}

func TestSoftwareCrossSlotRejected(t *testing.T) {
	store := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	amk, _ := GenerateAMK()
	_ = NewSoftwareSealer(store, 0).Seal("pin", amk)

	// Copy slot0's blob into slot1 and try to open it as slot1: the AAD binding must reject it.
	blob0, _ := store.Wrapped(0)
	_ = store.SetWrapped(1, blob0)
	if _, err := NewSoftwareSealer(store, 1).Unseal("pin"); err != ErrWrongPIN {
		t.Fatalf("cross-slot blob must be rejected via AAD, got %v", err)
	}
}

func TestSoftwareDestroy(t *testing.T) {
	store := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	s := NewSoftwareSealer(store, 0)
	amk, _ := GenerateAMK()
	_ = s.Seal("pin", amk)
	if err := s.Destroy(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Unseal("pin"); err == nil {
		t.Fatal("destroyed slot must not unseal")
	}
}
