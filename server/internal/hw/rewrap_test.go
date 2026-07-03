package hw

import (
	"bytes"
	"path/filepath"
	"testing"
)

// TestReWrapSoftwareToSoftware pins the verify-then-commit core with two software sealers over
// separate stores (standing in for the two tiers , the TPM side needs hardware). The property that
// matters: after ReWrap, BOTH wrappings recover the same AMK, and the source is untouched.
func TestReWrapPreservesAMKAndSource(t *testing.T) {
	fromStore := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	toStore := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	from := NewSoftwareSealer(fromStore, 0)
	to := NewSoftwareSealer(toStore, 0)

	amk, _ := GenerateAMK()
	if err := from.Seal("pin", amk); err != nil {
		t.Fatal(err)
	}
	if err := ReWrap(from, to, "pin"); err != nil {
		t.Fatal(err)
	}
	// both tiers hold the same key; source untouched (crash before mode-flip stays unlockable)
	a, err := from.Unseal("pin")
	if err != nil {
		t.Fatalf("source wrapping must survive ReWrap: %v", err)
	}
	b, err := to.Unseal("pin")
	if err != nil {
		t.Fatalf("target must unseal after ReWrap: %v", err)
	}
	if !bytes.Equal(a, amk) || !bytes.Equal(b, amk) {
		t.Fatal("AMK changed across ReWrap , the disk would be unreadable")
	}
}

func TestReWrapWrongPINLeavesTargetEmpty(t *testing.T) {
	fromStore := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	toStore := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	from := NewSoftwareSealer(fromStore, 0)
	amk, _ := GenerateAMK()
	_ = from.Seal("right", amk)

	if err := ReWrap(from, NewSoftwareSealer(toStore, 0), "wrong"); err == nil {
		t.Fatal("wrong PIN must fail ReWrap")
	}
	if w, _ := toStore.Wrapped(0); len(w) != 0 {
		t.Fatal("failed ReWrap must not leave a target wrapping behind")
	}
}
