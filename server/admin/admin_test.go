package admin

import "testing"

func TestLocalAddrAcceptsPrivateRejectsPublic(t *testing.T) {
	local := []string{"127.0.0.1", "127.0.0.1:22", "10.0.0.5", "172.16.0.1", "172.31.255.255",
		"192.168.1.50:54321", "169.254.1.1", "::1", "fe80::1", "fd12::3456"}
	for _, a := range local {
		if !IsLocalAddr(a) {
			t.Fatalf("should be local: %s", a)
		}
	}
	public := []string{"8.8.8.8", "1.1.1.1", "172.32.0.1", "172.15.255.255", "192.169.0.1",
		"203.0.113.7", "2001:4860:4860::8888"}
	for _, a := range public {
		if IsLocalAddr(a) {
			t.Fatalf("should be refused: %s", a)
		}
	}
}

func TestLocalAddrFailsClosed(t *testing.T) {
	for _, a := range []string{"", "notanip", "999.999.999.999", "localhost", "example.com"} {
		if IsLocalAddr(a) {
			t.Fatalf("must fail closed on: %q", a)
		}
	}
}

func TestRequireLocalCannotDetermineRefuses(t *testing.T) {
	if err := RequireLocal("", ""); err != ErrNotLocal {
		t.Fatal("no determinable origin must refuse")
	}
	if err := RequireLocal("", "8.8.8.8 5000 22"); err != ErrNotLocal {
		t.Fatal("public SSH_CLIENT must refuse")
	}
	if err := RequireLocal("192.168.1.5:22", ""); err != nil {
		t.Fatalf("local connPeer must pass: %v", err)
	}
}

type fakeBackend struct {
	committed bool
	pinCleared bool
}

func (f *fakeBackend) SlotSizeBytes(Slot) (uint64, error) { return 200 << 30, nil }
func (f *fakeBackend) CommitReset(Slot, string) error     { f.committed = true; return nil }
func (f *fakeBackend) ClearPinMemory()                    { f.pinCleared = true }

func TestResetupDestroysOnlyAfterConfirm(t *testing.T) {
	b := &fakeBackend{}
	r := NewResetup(b)

	// Mismatch: abort, no destroy, pin cleared.
	if err := r.Commit(SlotMain, "1111", "2222", "192.168.1.5", ""); err != ErrMismatch {
		t.Fatalf("mismatch must abort: %v", err)
	}
	if b.committed {
		t.Fatal("a mismatched PIN must NOT destroy the volume")
	}
	if !b.pinCleared {
		t.Fatal("PIN memory must be cleared even on abort")
	}

	// Non-local: refuse before any work.
	b2 := &fakeBackend{}
	r2 := NewResetup(b2)
	if err := r2.Commit(SlotMain, "1111", "1111", "8.8.8.8", ""); err != ErrNotLocal {
		t.Fatal("non-local commit must refuse")
	}
	if b2.committed {
		t.Fatal("non-local must not destroy")
	}

	// Confirmed + local: destroy happens.
	b3 := &fakeBackend{}
	r3 := NewResetup(b3)
	if err := r3.Commit(SlotMain, "1111", "1111", "192.168.1.5", ""); err != nil {
		t.Fatalf("confirmed local commit: %v", err)
	}
	if !b3.committed {
		t.Fatal("a confirmed local commit must destroy+recreate")
	}
}

func TestPrepareRefusesNonLocalAndTouchesNothing(t *testing.T) {
	b := &fakeBackend{}
	r := NewResetup(b)
	if _, err := r.Prepare(SlotDecoy, "8.8.8.8:22", ""); err != ErrNotLocal {
		t.Fatal("prepare from non-local must refuse")
	}
	w, err := r.Prepare(SlotDecoy, "192.168.1.5:22", "")
	if err != nil || w.SizeBytes == 0 {
		t.Fatalf("prepare local must return the warning with size: %v", err)
	}
	if b.committed {
		t.Fatal("prepare must never destroy")
	}
}

func TestSSHDLocalOnly(t *testing.T) {
	if ok, _ := SSHDIsLocalOnly("listenaddress 127.0.0.1:22"); !ok {
		t.Fatal("loopback bind should be local-only")
	}
	ok, public := SSHDIsLocalOnly("listenaddress 0.0.0.0:22")
	if ok || len(public) != 1 {
		t.Fatal("wildcard bind must be flagged public")
	}
	if ok, _ := SSHDIsLocalOnly(""); ok {
		t.Fatal("unknown config must fail closed (not local-only)")
	}
}

type fakeChangeBackend struct {
	rewrapped  bool
	pinCleared bool
	wrongPin   bool
}

func (f *fakeChangeBackend) RewrapKey(slot Slot, oldPin, newPin string) error {
	if f.wrongPin {
		return ErrWrongPin
	}
	f.rewrapped = true
	return nil
}
func (f *fakeChangeBackend) ClearPinMemory() { f.pinCleared = true }

func TestChangePinRewrapsOnlyWhenValid(t *testing.T) {
	// Confirmed, local, different new PIN -> rewrap happens.
	b := &fakeChangeBackend{}
	c := NewChangePin(b)
	if err := c.Run(SlotMain, "1111", "2222", "2222", "192.168.1.5", ""); err != nil {
		t.Fatalf("valid change: %v", err)
	}
	if !b.rewrapped || !b.pinCleared {
		t.Fatal("a valid change must rewrap and clear the PIN")
	}
}

func TestChangePinGuards(t *testing.T) {
	cases := []struct {
		name                     string
		old, new, confirm, peer  string
		want                     error
	}{
		{"non-local", "1111", "2222", "2222", "8.8.8.8", ErrNotLocal},
		{"mismatch", "1111", "2222", "3333", "192.168.1.5", ErrMismatch},
		{"same pin", "1111", "1111", "1111", "192.168.1.5", ErrSamePin},
		{"empty new", "1111", "", "", "192.168.1.5", ErrEmptyPin},
	}
	for _, tc := range cases {
		b := &fakeChangeBackend{}
		c := NewChangePin(b)
		if err := c.Run(SlotMain, tc.old, tc.new, tc.confirm, tc.peer, ""); err != tc.want {
			t.Fatalf("%s: want %v got %v", tc.name, tc.want, err)
		}
		if b.rewrapped {
			t.Fatalf("%s: must not rewrap on a guard failure", tc.name)
		}
	}
}

func TestChangePinWrongCurrentPinChangesNothing(t *testing.T) {
	b := &fakeChangeBackend{wrongPin: true}
	c := NewChangePin(b)
	if err := c.Run(SlotMain, "wrong", "2222", "2222", "192.168.1.5", ""); err != ErrWrongPin {
		t.Fatalf("wrong current PIN must return ErrWrongPin, got %v", err)
	}
	if b.rewrapped {
		t.Fatal("a wrong current PIN must change nothing")
	}
}
