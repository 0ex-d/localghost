package profile

import "testing"

func boxSalt() []byte { return []byte("test-box-salt-0001") }

func newReg(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(boxSalt())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestProfileAndDuressResolve(t *testing.T) {
	r := newReg(t)
	must(t, r.AddProfile("1111", 0)) // main in slot 0
	must(t, r.AddProfile("2222", 1)) // decoy in slot 1
	must(t, r.AddDuress("8888", 1, 0)) // open decoy 1, wipe main 0

	if got := r.Resolve("1111"); !got.Valid || got.Open != 0 || got.Wipe != NoSlot {
		t.Fatalf("main: %+v", got)
	}
	if got := r.Resolve("8888"); !got.Valid || got.Open != 1 || got.Wipe != 0 {
		t.Fatalf("duress should open decoy 1 and wipe main 0: %+v", got)
	}
	if got := r.Resolve("0000"); got.Valid {
		t.Fatalf("unknown PIN must be invalid: %+v", got)
	}
}

func TestCountIsHidden(t *testing.T) {
	r := newReg(t)
	// On-disk entry count is fixed regardless of how many real PINs exist.
	if len(r.entries) != RegistrySize {
		t.Fatalf("registry must always hold %d entries, got %d", RegistrySize, len(r.entries))
	}
	must(t, r.AddProfile("1111", 0))
	if len(r.entries) != RegistrySize {
		t.Fatal("adding a profile must not change the on-disk entry count")
	}
}

func TestPinReuseRejected(t *testing.T) {
	r := newReg(t)
	must(t, r.AddProfile("1111", 0))
	if err := r.AddProfile("1111", 1); err != ErrPinReused {
		t.Fatalf("PIN reuse must be rejected, got %v", err)
	}
}

func TestFillerNeverMatches(t *testing.T) {
	r := newReg(t)
	must(t, r.AddProfile("1111", 0))
	for _, pin := range []string{"0000", "9999", "abcd", "0001", "5555"} {
		if r.Resolve(pin).Valid {
			t.Fatalf("non-registered PIN %q must not match filler", pin)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
