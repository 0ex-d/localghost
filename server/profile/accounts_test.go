package profile

import (
	"testing"

	"github.com/LocalGhostDao/localghost/server/auth"
	"github.com/LocalGhostDao/localghost/server/wipe"
)

func buildAccounts(t *testing.T) *Accounts {
	t.Helper()
	s, err := NewSetup([]byte("box-salt"))
	must(t, err)
	must(t, s.SetMain("1111"))
	must(t, s.AddDecoy("2222", false)) // slot 1, plain decoy
	must(t, s.AddDecoy("3333", true))  // slot 2, duress decoy (wipes main)
	reg, err := s.Finalize()
	must(t, err)

	gate := auth.NewGate(auth.DefaultPolicy(), auth.NewMemoryStore())
	wiper := wipe.NewWiper(wipe.NewKeyVault(), nil, nil)
	return NewAccounts(reg, gate, wiper)
}

func TestUnlockMain(t *testing.T) {
	d := buildAccounts(t).Unlock("dev", "1111")
	if d.Outcome != Open || d.OpenSlot != MainSlot || d.MainWiped {
		t.Fatalf("main: %+v", d)
	}
}

func TestUnlockPlainDecoyNoWipe(t *testing.T) {
	d := buildAccounts(t).Unlock("dev", "2222")
	if d.Outcome != Open || d.OpenSlot != 1 || d.MainWiped {
		t.Fatalf("plain decoy should open slot 1 without wiping: %+v", d)
	}
}

func TestUnlockDuressDecoyWipesMain(t *testing.T) {
	d := buildAccounts(t).Unlock("dev", "3333")
	if d.Outcome != Open || d.OpenSlot != 2 || !d.MainWiped {
		t.Fatalf("duress decoy should open slot 2 and wipe main: %+v", d)
	}
}

func TestUnlockInvalid(t *testing.T) {
	if d := buildAccounts(t).Unlock("dev", "0000"); d.Outcome != Reject {
		t.Fatalf("invalid: %+v", d)
	}
}

func TestPolicyRejectsThirdDecoy(t *testing.T) {
	s, err := NewSetup([]byte("s"))
	must(t, err)
	must(t, s.SetMain("1111"))
	must(t, s.AddDecoy("a", false))
	must(t, s.AddDecoy("b", true))
	if err := s.AddDecoy("c", false); err != ErrTooMany {
		t.Fatalf("third decoy must be rejected, got %v", err)
	}
}

func TestFinalizeRequiresMainAndDecoy(t *testing.T) {
	s, _ := NewSetup([]byte("s"))
	if _, err := s.Finalize(); err != ErrNoMain {
		t.Fatalf("want ErrNoMain, got %v", err)
	}
	must(t, s.SetMain("1111"))
	if _, err := s.Finalize(); err != ErrNoDecoy {
		t.Fatalf("want ErrNoDecoy, got %v", err)
	}
}
