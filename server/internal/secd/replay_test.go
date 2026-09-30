package secd

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/profile"
)

// fakeBox is an UnlockBackend that opens slot 0 for "1111", warm or cold, at once.
type fakeBox struct{ warm bool }

func (f fakeBox) Resolve(pin string) (int, bool, error) {
	if pin == "1111" {
		return profile.MainSlot, false, nil
	}
	return profile.NoSlot, false, nil
}
func (fakeBox) Unseal(int, string) ([]byte, error)     { return []byte("k"), nil }
func (fakeBox) Mount(int, []byte) error                { time.Sleep(20 * time.Millisecond); return nil }
func (fakeBox) StartDB(int) error                      { return nil }
func (fakeBox) StartCache(int) error                   { return nil }
func (f fakeBox) Warm(int) bool                        { return f.warm }
func (fakeBox) Lock(int, func(profile.Progress)) error { return nil }
func (fakeBox) AuthorizesLock(string) bool             { return false }

// a volume that counts as mounted
func withVolume(t *testing.T) string {
	t.Helper()
	old := isMountPoint
	isMountPoint = func(string) bool { return true }
	t.Cleanup(func() { isMountPoint = old })
	path := filepath.Join(t.TempDir(), "mnt", "slot0", "secd", "unlock-times.json")
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(path)), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// A cold unlock is shown as it happens and recorded; MODEL does not hold it.
func TestColdUnlockIsRecorded(t *testing.T) {
	path := withVolume(t)
	u := newUnlockService(fakeBox{warm: false})
	u.timesPath = path
	u.run("1111")
	if !u.done || u.progress[profile.StageMount] != profile.Complete || u.progress[profile.StageModel] != profile.Skipped {
		t.Fatalf("done %v, mount %v, model %v", u.done, u.progress[profile.StageMount], u.progress[profile.StageModel])
	}
	list := loadUnlockTimes(path)
	if len(list) != 1 || len(list[0]) != len(replaySteps) || list[0][2] < 20 {
		t.Fatalf("recorded %v", list)
	}
	// eight kept, the oldest go
	for i := 0; i < 10; i++ {
		saveUnlockTimes(path, appendUnlockTimes(loadUnlockTimes(path), unlockTimes{int64(i), 1, 1, 1, 1, 1}))
	}
	if l := loadUnlockTimes(path); len(l) != keptUnlocks || l[0][0] != 2 {
		t.Fatalf("kept %v", l)
	}
}

// A warm unlock replays a kept cold one: every step completes (none skipped), in order, and the
// whole takes the kept length within 8%.
func TestWarmUnlockReplaysAColdOne(t *testing.T) {
	path := withVolume(t)
	saveUnlockTimes(path, []unlockTimes{{100, 100, 200, 100, 100, 100}}) // 700 ms
	u := newUnlockService(fakeBox{warm: true})
	u.timesPath = path
	var mu sync.Mutex
	var seen []profile.Progress
	stop := make(chan struct{})
	go func() { // watch the poll's view every 10 ms
		last := map[profile.Stage]profile.StepState{}
		for {
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
			u.mu.Lock()
			for st, s := range u.progress {
				if last[st] != s {
					last[st] = s
					mu.Lock()
					seen = append(seen, profile.Progress{Stage: st, State: s})
					mu.Unlock()
				}
			}
			u.mu.Unlock()
		}
	}()
	t0 := time.Now()
	u.run("1111")
	took := time.Since(t0)
	close(stop)
	if took < 640*time.Millisecond || took > 900*time.Millisecond {
		t.Fatalf("a warm unlock took %v, want about 700 ms", took)
	}
	for _, st := range []profile.Stage{profile.StageUnseal, profile.StageMount} {
		if u.progress[st] != profile.Complete {
			t.Fatalf("%v shows %v, a cold unlock shows complete", st, u.progress[st])
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range seen {
		if p.State == profile.Skipped && p.Stage != profile.StageModel {
			t.Fatalf("a warm unlock showed %v skipped", p.Stage)
		}
	}
	if len(loadUnlockTimes(path)) != 1 {
		t.Fatal("a warm unlock was recorded as a cold one")
	}
}

// A wrong PIN on a warm box answers like one on a cold box: at the reject time, as it is.
func TestWarmRejectIsNotReplayed(t *testing.T) {
	old := rejectFloor
	rejectFloor = func() time.Duration { return 50 * time.Millisecond }
	defer func() { rejectFloor = old }()
	u := newUnlockService(fakeBox{warm: true})
	t0 := time.Now()
	u.run("0000")
	if u.done || u.failed == "" || u.progress[profile.StageResolve] != profile.Errored || time.Since(t0) > 300*time.Millisecond {
		t.Fatalf("done %v failed %q resolve %v in %v", u.done, u.failed, u.progress[profile.StageResolve], time.Since(t0))
	}
}

func TestPickScalesWithinEightPercent(t *testing.T) {
	base := unlockTimes{1000, 1000, 1000, 1000, 1000, 1000}
	for i := 0; i < 200; i++ {
		p := pickUnlockTimes([]unlockTimes{base})
		for _, ms := range p {
			if ms < 919 || ms > 1081 {
				t.Fatalf("scaled to %d", ms)
			}
		}
	}
	if p := pickUnlockTimes(nil); len(p) != len(replaySteps) {
		t.Fatalf("default %v", p)
	}
}

// Nothing is written when the volume is not mounted.
func TestTimesStayOffTheOSDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mnt", "slot0", "secd", "unlock-times.json")
	_ = os.MkdirAll(filepath.Dir(filepath.Dir(path)), 0o755) // the mount point exists, nothing mounted
	saveUnlockTimes(path, []unlockTimes{{1, 1, 1, 1, 1, 1}})
	if _, err := os.Stat(path); err == nil {
		t.Fatal("written with nothing mounted")
	}
}
