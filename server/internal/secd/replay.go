package secd

// THE COLD UNLOCKS A WARM ONE REPLAYS (1 Oct 2026). A cold unlock's step times are kept on the
// encrypted volume, the last eight; a warm unlock picks one at random, stretches or shrinks it by
// up to 8%, and walks the app through it (unlockService.run). In a model of xyntai the best
// stopwatch threshold then tells warm from cold 50.6% of the time, against 99.96% with a flat
// 5-to-10-second floor (the privacy field notes of 30 Sep 2026). Nothing here is logged.

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/profile"
)

// keptUnlocks: how many cold unlocks are kept to choose from.
const keptUnlocks = 8

// replaySteps are the steps a cold unlock times and a warm one replays, in order. MODEL is off the
// unlock (the model loads after it) and READY is the arrival.
var replaySteps = []profile.Stage{
	profile.StageResolve, profile.StageUnseal, profile.StageMount,
	profile.StageStartDB, profile.StageStartCache, profile.StageDaemons,
}

// unlockTimes is one cold unlock: each step's time in ms, in replaySteps order.
type unlockTimes []int64

// defaultUnlockTimes stands in until this box has timed a cold unlock of its own (ms): a TPM
// unseal, a LUKS open and mount, the two databases, the cohort.
var defaultUnlockTimes = unlockTimes{400, 1000, 2400, 700, 900, 1600}

// coldTimes turns the moments each step finished (from the start) into step times. False when a
// step is missing (not a full cold unlock).
func coldTimes(ends map[profile.Stage]time.Duration) (unlockTimes, bool) {
	out := make(unlockTimes, 0, len(replaySteps))
	var prev time.Duration
	for _, st := range replaySteps {
		e, ok := ends[st]
		if !ok || e < prev {
			return nil, false
		}
		out = append(out, (e - prev).Milliseconds())
		prev = e
	}
	return out, true
}

func appendUnlockTimes(list []unlockTimes, t unlockTimes) []unlockTimes {
	list = append(list, t)
	if len(list) > keptUnlocks {
		list = list[len(list)-keptUnlocks:]
	}
	return list
}

func loadUnlockTimes(path string) []unlockTimes {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var list []unlockTimes
	if json.Unmarshal(b, &list) != nil {
		return nil
	}
	ok := list[:0]
	for _, t := range list {
		if len(t) == len(replaySteps) {
			ok = append(ok, t)
		}
	}
	return ok
}

// saveUnlockTimes writes the list, but only onto a mounted volume: the file's directory is made
// inside the mount, and a mount that is not there leaves nothing on the OS disk.
func saveUnlockTimes(path string, list []unlockTimes) {
	if path == "" {
		return
	}
	mount := filepath.Dir(filepath.Dir(path))
	if st, err := os.Stat(mount); err != nil || !st.IsDir() || !isMountPoint(mount) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	b, _ := json.Marshal(list)
	if os.WriteFile(path+".tmp", b, 0o600) == nil {
		_ = os.Rename(path+".tmp", path)
	}
}

// isMountPoint: the directory is on a different device from its parent. A variable for tests.
var isMountPoint = func(dir string) bool {
	a, err1 := os.Stat(dir)
	b, err2 := os.Stat(filepath.Dir(dir))
	if err1 != nil || err2 != nil {
		return false
	}
	return devOf(a) != devOf(b)
}

// pickUnlockTimes: one of the kept cold unlocks at random (the built-in one when there are none),
// every step scaled by the same factor in 0.92..1.08.
func pickUnlockTimes(list []unlockTimes) unlockTimes {
	base := defaultUnlockTimes
	if len(list) > 0 {
		base = list[randN(len(list))]
	}
	f := 0.92 + 0.16*float64(randN(10001))/10000
	out := make(unlockTimes, len(base))
	for i, ms := range base {
		out[i] = int64(float64(ms) * f)
	}
	return out
}

func randN(n int) int {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int(binary.LittleEndian.Uint64(b[:]) % uint64(n))
}

// replayUnlock walks the app through t from start: RESOLVE is already shown running; each step
// then completes at its moment and the next shows running, as a cold unlock would. sleep is
// time.Sleep outside tests.
func replayUnlock(start time.Time, t unlockTimes, publish func(profile.Progress), sleep func(time.Duration)) {
	var at time.Duration
	for i, st := range replaySteps {
		if i > 0 {
			publish(profile.Progress{Stage: st, State: profile.Running})
		}
		if i < len(t) {
			at += time.Duration(t[i]) * time.Millisecond
		}
		if d := time.Until(start.Add(at)); d > 0 {
			sleep(d)
		}
		publish(profile.Progress{Stage: st, State: profile.Complete})
	}
}
