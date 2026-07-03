package hw

import (
	"path/filepath"
	"strings"
	"testing"
)

// SelectSealer is the security-critical branch: it must never hand back a software sealer for a
// tpm-provisioned box (that would be a silent downgrade to a tier that cannot even unseal the
// hardware-sealed key), and it must refuse an unprovisioned box. The TPM-present cases need a device
// and are exercised on the box; here we pin the tier LOGIC that does not need hardware.

func TestSelectSealerSoftwareNeverProbesTPM(t *testing.T) {
	store := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	// A bogus device path: if software mode probed the TPM, this would matter. It must not.
	s, err := SelectSealer(SealModeSoftware, "/dev/does-not-exist", store, 0)
	if err != nil {
		t.Fatalf("software mode must not depend on a TPM device: %v", err)
	}
	if _, ok := s.(*SoftwareSealer); !ok {
		t.Fatalf("software mode must return a SoftwareSealer, got %T", s)
	}
}

func TestSelectSealerUnprovisionedErrors(t *testing.T) {
	store := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	if _, err := SelectSealer("", "/dev/tpmrm0", store, 0); err == nil {
		t.Fatal("empty mode (unprovisioned) must error, not pick a tier")
	}
}

func TestSelectSealerUnknownModeErrors(t *testing.T) {
	store := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	if _, err := SelectSealer("banana", "/dev/tpmrm0", store, 0); err == nil {
		t.Fatal("unknown mode must error")
	}
}

func TestSelectSealerTPMModeWithNoDeviceRefusesNotDowngrades(t *testing.T) {
	store := NewEnvSealStore(filepath.Join(t.TempDir(), "seal.env"))
	// tpm mode + a device that cannot open must return an ERROR, never a SoftwareSealer , the whole
	// point of "never silently downgrade".
	s, err := SelectSealer(SealModeTPM, "/dev/does-not-exist", store, 0)
	if err == nil {
		t.Fatal("tpm mode with an unusable device must error, not fall back")
	}
	if s != nil {
		t.Fatalf("tpm mode with an unusable device must return a nil sealer, got %T", s)
	}
	if !strings.Contains(err.Error(), "not a fall-back") && !strings.Contains(err.Error(), "not usable") {
		t.Fatalf("error should explain the no-downgrade stance, got: %v", err)
	}
}
