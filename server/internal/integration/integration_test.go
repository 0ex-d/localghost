package integration

import "testing"

func TestAddStartsPaused(t *testing.T) {
	s := NewSet(0, false)
	it, err := s.Add("monzo", Bank, "Monzo", []byte("tok"))
	if err != nil {
		t.Fatal(err)
	}
	if it.State != Paused {
		t.Fatal("a new integration must start paused")
	}
}

func TestEnableOnMain(t *testing.T) {
	s := NewSet(0, false)
	s.Add("monzo", Bank, "Monzo", []byte("tok"))
	if err := s.Enable("monzo"); err != nil {
		t.Fatalf("enable on main should work: %v", err)
	}
	if len(s.Active()) != 1 {
		t.Fatal("enabled connector should be active")
	}
}

func TestDecoyStaysPaused(t *testing.T) {
	s := NewSet(1, true) // decoy
	s.Add("fakebank", Bank, "Some Bank", []byte("tok"))
	if err := s.Enable("fakebank"); err != ErrDecoyStaysPaused {
		t.Fatalf("decoy enable must be refused, got %v", err)
	}
	if len(s.Active()) != 0 {
		t.Fatal("a decoy must have no active (polling) integrations")
	}
}

func TestDecoyNeverLoadsEnabled(t *testing.T) {
	// Even if the stored blob claims Enabled, a decoy loads everything Paused.
	main := NewSet(0, false)
	main.Add("monzo", Bank, "Monzo", []byte("tok"))
	main.Enable("monzo")
	blob, err := main.Save()
	if err != nil {
		t.Fatal(err)
	}
	// Load the SAME blob as if into a decoy slot.
	decoy, err := Load(1, blob)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range decoy.All() {
		if it.State != Paused {
			t.Fatal("a decoy must force every integration to paused on load")
		}
	}
}

func TestMainSlotIsNotDecoy(t *testing.T) {
	s, err := Load(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.IsDecoy() {
		t.Fatal("slot 0 must be the main, not a decoy")
	}
}
