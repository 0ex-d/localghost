package profile

import (
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/auth"
	"github.com/LocalGhostDao/localghost/server/internal/wipe"
)

func buildAccounts(t *testing.T) *Accounts {
	t.Helper()
	s, err := NewSetup([]byte("box-salt"))
	must(t, err)
	must(t, s.SetMain("1111")) // main account, slot 0
	must(t, s.SetWipe("3333")) // wipe PIN, crypto-erases everything
	reg, err := s.Finalize()
	must(t, err)

	gate := auth.NewGate(auth.DefaultPolicy(), auth.NewMemoryStore())
	wiper := wipe.NewWiper(wipe.NewKeyVault(), nil, nil)
	return NewAccounts(reg, gate, wiper)
}

func TestUnlockMain(t *testing.T) {
	d := buildAccounts(t).Unlock("dev", "1111")
	if d.Outcome != Open || d.OpenSlot != MainSlot || d.Wiped {
		t.Fatalf("main should open slot 0 without wiping: %+v", d)
	}
}

func TestWipePinAloneArmsButDoesNotErase(t *testing.T) {
	a := buildAccounts(t)
	// The wipe PIN on its own must NOT erase , it only arms. It looks exactly like a wrong PIN.
	d := a.Unlock("dev", "3333")
	if d.Outcome != Reject || d.Wiped {
		t.Fatalf("wipe PIN alone should arm, not erase, and look like a reject: %+v", d)
	}
}

func TestWipeConfirmedByMainErases(t *testing.T) {
	a := buildAccounts(t)
	a.Unlock("dev", "3333") // arm
	// Main PIN while armed CONFIRMS the wipe: erases, and still looks like a wrong PIN (not Open).
	d := a.Unlock("dev", "1111")
	if d.Outcome != Reject || !d.Wiped {
		t.Fatalf("main PIN after wipe PIN should confirm the erase and look like a reject: %+v", d)
	}
}

func TestMainAloneOpensWithoutArming(t *testing.T) {
	a := buildAccounts(t)
	d := a.Unlock("dev", "1111")
	if d.Outcome != Open || d.OpenSlot != MainSlot || d.Wiped {
		t.Fatalf("main PIN with nothing armed should open, not wipe: %+v", d)
	}
}

func TestWrongPinCancelsArmedWipe(t *testing.T) {
	a := buildAccounts(t)
	a.Unlock("dev", "3333") // arm
	a.Unlock("dev", "0000") // wrong PIN cancels the arm
	// Main PIN now opens normally , the pending wipe was cancelled.
	d := a.Unlock("dev", "1111")
	if d.Outcome != Open || d.Wiped {
		t.Fatalf("a wrong PIN must cancel the armed wipe so main opens: %+v", d)
	}
}

func TestArmedWipeExpires(t *testing.T) {
	a := buildAccounts(t)
	base := time.Unix(1_700_000_000, 0)
	a.now = func() time.Time { return base }
	a.Unlock("dev", "3333") // arm at base
	// Past the window, the arm is stale: main opens instead of confirming a wipe.
	a.now = func() time.Time { return base.Add(wipeArmWindow + time.Second) }
	d := a.Unlock("dev", "1111")
	if d.Outcome != Open || d.Wiped {
		t.Fatalf("an expired arm must not wipe; main should open: %+v", d)
	}
}

func TestArmIsPerDevice(t *testing.T) {
	a := buildAccounts(t)
	a.Unlock("devA", "3333") // arm on device A only
	// Main PIN from a DIFFERENT device must not complete A's sequence , it opens normally.
	d := a.Unlock("devB", "1111")
	if d.Outcome != Open || d.Wiped {
		t.Fatalf("wipe armed on device A must not be confirmable from device B: %+v", d)
	}
}

func TestUnlockInvalid(t *testing.T) {
	d := buildAccounts(t).Unlock("dev", "0000")
	if d.Outcome != Reject || d.Wiped {
		t.Fatalf("invalid PIN should reject without wiping: %+v", d)
	}
}

func TestFinalizeRequiresMain(t *testing.T) {
	s, _ := NewSetup([]byte("s"))
	if _, err := s.Finalize(); err != ErrNoMain {
		t.Fatalf("want ErrNoMain, got %v", err)
	}
	must(t, s.SetMain("1111"))
	// Wipe PIN is OPTIONAL now, so finalize must succeed with just a main PIN.
	if _, err := s.Finalize(); err != nil {
		t.Fatalf("main-only setup should finalize, got %v", err)
	}
}

func TestWipePinOptionalButOnce(t *testing.T) {
	s, _ := NewSetup([]byte("s"))
	must(t, s.SetMain("1111"))
	must(t, s.SetWipe("2222"))
	if err := s.SetWipe("3333"); err != ErrWipeSet {
		t.Fatalf("setting a second wipe PIN must be rejected, got %v", err)
	}
}
