package secd

import (
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/profile"
)

// fakeResolve is an UnlockBackend whose Resolve takes a set time and rejects.
type fakeResolve struct{ took time.Duration }

func (f fakeResolve) Resolve(string) (int, bool, error) {
	time.Sleep(f.took)
	return profile.NoSlot, f.took > 0, nil
}
func (fakeResolve) Unseal(int, string) ([]byte, error)     { return nil, nil }
func (fakeResolve) Mount(int, []byte) error                { return nil }
func (fakeResolve) StartDB(int) error                      { return nil }
func (fakeResolve) StartCache(int) error                   { return nil }
func (fakeResolve) Warm(int) bool                          { return false }
func (fakeResolve) Lock(int, func(profile.Progress)) error { return nil }
func (fakeResolve) AuthorizesLock(string) bool             { return false }

// A throttled reject (0 ms) and a wipe confirmation (the KDF plus an erase, 300 ms here) are
// answered at the same moment.
func TestRejectsAnswerTogether(t *testing.T) {
	old := rejectFloor
	rejectFloor = func() time.Duration { return 400 * time.Millisecond }
	defer func() { rejectFloor = old }()
	when := func(took time.Duration) time.Duration {
		t0 := time.Now()
		var at time.Duration
		_, err := runUnlock(fakeResolve{took}, "0000", func(p profile.Progress) {
			if p.Stage == profile.StageResolve && p.State == profile.Errored {
				at = time.Since(t0)
			}
		})
		if err == nil {
			t.Fatal("not rejected")
		}
		return at
	}
	fast, slow := when(0), when(300*time.Millisecond)
	d := fast - slow
	if d < 0 {
		d = -d
	}
	if fast < 400*time.Millisecond || slow < 400*time.Millisecond || d > 40*time.Millisecond {
		t.Fatalf("throttled answered at %v, wipe at %v", fast, slow)
	}
}
