package integration

import "testing"

func TestAddStartsPaused(t *testing.T) {
	s := NewSet(0)
	it, err := s.Add("monzo", Bank, "Monzo", []byte("tok"))
	if err != nil {
		t.Fatal(err)
	}
	if it.State != Paused {
		t.Fatal("a new integration must start paused")
	}
}

func TestEnableMakesActive(t *testing.T) {
	s := NewSet(0)
	s.Add("monzo", Bank, "Monzo", []byte("tok"))
	if err := s.Enable("monzo"); err != nil {
		t.Fatalf("enable should work: %v", err)
	}
	if len(s.Active()) != 1 {
		t.Fatal("an enabled connector should be active")
	}
}

func TestDisablePauses(t *testing.T) {
	s := NewSet(0)
	s.Add("monzo", Bank, "Monzo", []byte("tok"))
	s.Enable("monzo")
	if err := s.Disable("monzo"); err != nil {
		t.Fatalf("disable should work: %v", err)
	}
	if len(s.Active()) != 0 {
		t.Fatal("a disabled connector must not be active")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := NewSet(0)
	s.Add("monzo", Bank, "Monzo", []byte("tok"))
	s.Enable("monzo")
	blob, err := s.Save()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(0, blob)
	if err != nil {
		t.Fatal(err)
	}
	all := loaded.All()
	if len(all) != 1 || all[0].ID != "monzo" || all[0].State != Enabled {
		t.Fatalf("round-trip should preserve the integration and its state: %+v", all)
	}
}
