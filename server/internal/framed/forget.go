package framed

// FORGET A SPOT. The questions (questions.go) catch what the rules can see; the person sees the
// rest on the map. Zoomed right in on a fix they know is wrong, the phone offers to delete it, and
// deleting one fix is not enough: a cell tower's position repeats for as long as the phone hangs
// on it, and the map's line (simplified) keeps only one of those fixes. So a delete takes the fix
// and the fixes either side of it in time that sit within a small radius of it, up to the first
// one somewhere else. The phone asks first without deleting (dry), shows how many and when, and
// deletes on the person's yes.

import (
	"fmt"
	"sort"
	"time"
)

const (
	spotRadiusM    = 150.0    // "the same spot", when the phone does not say
	spotMaxRadiusM = 1000.0   // and at most
	spotWindowS    = 6 * 3600 // how far either side of the fix a run of the same spot is looked for
	spotMaxFixes   = 200      // a spot that holds more than this is where the person lives, not a glitch
)

// SpotRun is the seconds of the fix at ts and of its neighbours in time within radiusM of it (the
// run stops at the first fix elsewhere), in time order; nil when no fix is at ts.
func SpotRun(pts []TrackPoint, ts int64, radiusM float64) []int64 {
	sorted := make([]TrackPoint, len(pts))
	copy(sorted, pts)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TS < sorted[j].TS })
	k := sort.Search(len(sorted), func(i int) bool { return sorted[i].TS >= ts })
	if k == len(sorted) || sorted[k].TS != ts {
		return nil
	}
	at := sorted[k]
	lo, hi := k, k
	for lo > 0 && HaversineM(at, sorted[lo-1]) <= radiusM {
		lo--
	}
	for hi+1 < len(sorted) && HaversineM(at, sorted[hi+1]) <= radiusM {
		hi++
	}
	var out []int64
	for i := lo; i <= hi; i++ {
		if len(out) == 0 || out[len(out)-1] != sorted[i].TS { // two sources at one second: one entry
			out = append(out, sorted[i].TS)
		}
	}
	return out
}

// ForgetSpot finds the run of fixes at the spot of the fix at ts (SpotRun) and, unless dry,
// deletes them from every source and rebuilds the days they touch. It returns the run's seconds
// and how many rows went.
func (p *Pipeline) ForgetSpot(ts int64, radiusM float64, dry bool) ([]int64, int, error) {
	if radiusM <= 0 {
		radiusM = spotRadiusM
	}
	if radiusM > spotMaxRadiusM {
		radiusM = spotMaxRadiusM
	}
	pts, err := p.store.DayPoints(ts-spotWindowS, ts+spotWindowS+1)
	if err != nil {
		return nil, 0, err
	}
	run := SpotRun(pts, ts, radiusM)
	if len(run) == 0 {
		return nil, 0, fmt.Errorf("no fix at %d", ts)
	}
	if len(run) > spotMaxFixes {
		return nil, 0, fmt.Errorf("%d fixes at that spot: that is a place, not a glitch", len(run))
	}
	if dry {
		return run, 0, nil
	}
	n, err := p.store.DeletePoints(run)
	if err != nil {
		return run, n, err
	}
	days := map[string]bool{}
	for _, t := range []int64{run[0], run[len(run)-1]} {
		days[time.Unix(t, 0).UTC().Format("2006-01-02")] = true
	}
	for d := range days {
		p.RebuildDay(d)
	}
	p.log.Info("trail spot forgotten", "fn", "ForgetSpot", "fixes", len(run), "deleted", n)
	return run, n, nil
}
