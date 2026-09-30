package framed

// QUESTIONS ABOUT THE TRAIL. The glitch rules (clean.go) hide what cannot be true; a stretch that
// only looks unlikely is drawn, and the person is the one who knows. So a day carries questions:
// "at 14:10 the trail goes to Kavos, 16 km away across the sea, and is back 30 minutes later. Were
// you there?" No deletes those points from the database for good; yes keeps them and the stretch is
// never asked about again (trail_kept). Asking costs a tap, missing one leaves a line to Corfu on
// the map, so the rules lean towards asking. The kinds:
//
//   glitch   , points the glitch rules already leave off the map. Asked so the person can delete
//              them for good; nothing changes on the map either way.
//   sea      , a hop of the trail runs across open water (seaAskM or more of it on the box's land
//              tiles, seaLongAskM when the run out there is longer than a few fixes) faster than a
//              boat goes, or out and back across it within the hour and a half;
//              an island's phone on the next island's cell tower draws exactly this, and no road gets
//              you there.
//   fast     , an out-and-back of a few fixes that needs fastAskMS (90 km/h) or more on the way out
//              or back: with a fix every quarter hour, 25 km out and back again is not a drive. At
//              the edge of the data, where there is no way back to judge, edgeFastMS (180 km/h).
//   offroad  , an out-and-back to a place with no road near it (the box's road tiles know the
//              area, and there is none within offRoadM), where the neighbours were somewhere the
//              phone could get to: the sea off an island, a mountainside off every path.
//
// An excursion is the run of fixes after a long hop that stays away from where the trail left,
// until one comes back near it. At the edge of the data (the first fixes of the window, or the
// last) there is nothing to come back to, so a run of a few fixes there, reached by a hop that
// fits one of the kinds, is asked about on its own. Visits to the same place in the same afternoon
// (a phone flipping between two islands' towers) are one question.
//
// Pure: the caller passes the road and land checks and the decisions already made.

import (
	"math"
	"sort"
)

const (
	fastAskMS    = 90.0 / 3.6  // 90 km/h, averaged over a leg between two fixes
	edgeFastMS   = 180.0 / 3.6 // at the edge of the data, where nothing says it came back: a motorway is not odd
	boatAskMS    = 35.0 / 3.6  // a water hop faster than this is faster than a ferry's usual pace
	askMinAwayM  = 3000.0      // a hop shorter than this is never asked about
	askGlitchM   = 2000.0      // a glitch closer than this is not worth a question
	askMaxPoints = 3           // fast and offroad: how many fixes out there make one excursion
	seaMaxPoints = 8           // sea: a tower across the water can hold the phone for a couple of hours
	seaMaxS      = 4 * 3600    // and how long the run may last before it is a real trip
	offRoadM     = 1000.0      // "no road near it"
	seaAskM      = 2500.0      // this much open water in a row on a hop is a crossing, not a bay
	seaLongAskM  = 5000.0      // for a run of more fixes: a coast road's chord cuts across bays, not straits
	seaStepM     = 250.0       // how finely a hop is sampled for water
	mergeNearM   = 3000.0      // two questions this close out there are the same place
	mergeGapS    = 3 * 3600    // and this close in time, the same afternoon
)

// Question is one stretch of a day's trail the person is asked about.
type Question struct {
	From  int64   `json:"from"` // the first point asked about (unix seconds)
	To    int64   `json:"to"`   // the last
	TS    []int64 `json:"ts"`   // every point asked about: what a "no" deletes
	Lat   float64 `json:"lat"`  // the far point
	Lon   float64 `json:"lon"`
	AwayM float64 `json:"awayM"`          // how far it is from where the trail left
	GoneS int64   `json:"goneS"`          // from leaving to being back (0 when the data ends out there)
	KMH   float64 `json:"kmh"`            // the faster of the two legs
	SeaM  float64 `json:"seaM,omitempty"` // open water crossed on the way, when that is why it is asked
	Kind  string  `json:"kind"`           // glitch | sea | fast | offroad
	Place string  `json:"place,omitempty"`
}

// AskChecks is what the box knows about the ground, and what the person already decided. Any may
// be nil: a nil check never makes a question.
type AskChecks struct {
	// NearRoad: does a road pass within withinM of the point, and can the box tell
	NearRoad func(lat, lon, withinM float64) (near, known bool)
	// OnLand: is the point on land, and can the box tell (the land tiles)
	OnLand func(lat, lon float64) (land, known bool)
	// Kept: did the person already say yes to a stretch that overlaps [from, to]
	Kept func(from, to int64) bool
}

// TrailQuestions finds the stretches of the day [dayStart, dayEnd) worth asking about in raw (the
// day's stored points, with a couple of hours either side for context).
func TrailQuestions(raw []TrackPoint, dayStart, dayEnd int64, ck AskChecks) []Question {
	if len(raw) < 2 {
		return nil
	}
	pts := make([]TrackPoint, len(raw))
	copy(pts, raw)
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].TS < pts[j].TS })
	cleaned, _ := CleanTrack(pts)
	var out []Question
	add := func(q Question) {
		if q.From < dayStart || q.From >= dayEnd {
			return
		}
		if ck.Kept != nil && ck.Kept(q.From, q.To) {
			return
		}
		for _, o := range out { // the same fixes found from both ends of the data
			if o.From == q.From && o.To == q.To {
				return
			}
		}
		out = append(out, q)
	}

	// 1. the glitch rules' drops, as runs between the points that survived
	type key struct {
		ts       int64
		lat, lon float64
	}
	survived := make(map[key]int, len(cleaned))
	for _, p := range cleaned {
		survived[key{p.TS, p.Lat, p.Lon}]++
	}
	isKept := make([]bool, len(pts))
	for i, p := range pts {
		k := key{p.TS, p.Lat, p.Lon}
		if survived[k] > 0 {
			survived[k]--
			isKept[i] = true
		}
	}
	for i := 0; i < len(pts); {
		if isKept[i] {
			i++
			continue
		}
		j := i
		for j < len(pts) && !isKept[j] {
			j++
		}
		// pts[i:j] were dropped; the trail left from i-1 and came back at j
		var from, back *TrackPoint
		if i > 0 {
			from = &pts[i-1]
		}
		if j < len(pts) {
			back = &pts[j]
		}
		if from != nil || back != nil {
			q := excursion(pts[i:j], from, back, "glitch")
			if q.AwayM >= askGlitchM {
				add(q)
			}
		}
		i = j
	}

	// 2. excursions among the points that are drawn, forwards; then the data's start, backwards
	for _, q := range excursions(cleaned, ck, false) {
		add(q)
	}
	rev := make([]TrackPoint, len(cleaned))
	for i, p := range cleaned {
		rev[len(cleaned)-1-i] = p
	}
	for _, q := range excursions(rev, ck, true) {
		add(q)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].From < out[j].From })
	return mergeVisits(out)
}

// mergeVisits folds a run of questions about the same place into one: a phone that flips between
// two islands' towers goes to Kavos five times in an afternoon, and that is one "no", not five.
// Same kind, far points within mergeNearM (or a fifth of the way out), each within mergeGapS of
// the last.
func mergeVisits(qs []Question) []Question {
	if len(qs) < 2 {
		return qs
	}
	out := []Question{qs[0]}
	for _, q := range qs[1:] {
		m := &out[len(out)-1]
		near := math.Max(mergeNearM, 0.2*math.Max(m.AwayM, q.AwayM))
		if q.Kind != m.Kind || q.From-m.To > mergeGapS ||
			HaversineM(TrackPoint{Lat: m.Lat, Lon: m.Lon}, TrackPoint{Lat: q.Lat, Lon: q.Lon}) > near {
			out = append(out, q)
			continue
		}
		if q.GoneS <= 0 || m.GoneS <= 0 {
			m.GoneS = 0 // the data ends out there
		} else {
			m.GoneS = q.From - m.From + q.GoneS
		}
		m.TS = append(m.TS, q.TS...)
		if q.AwayM > m.AwayM {
			m.Lat, m.Lon, m.AwayM, m.Place = q.Lat, q.Lon, q.AwayM, q.Place
		}
		m.KMH, m.SeaM = math.Max(m.KMH, q.KMH), math.Max(m.SeaM, q.SeaM)
		*m = normalise(*m)
	}
	return out
}

// excursions walks the drawn points: after each hop of at least askMinAwayM, the run of fixes that
// stays away from where the trail left, up to the first one back near it. edgeOnly keeps just the
// runs the data ends in (used on the reversed points, where that end is the data's start).
func excursions(c []TrackPoint, ck AskChecks, edgeOnly bool) []Question {
	var out []Question
	for i := 1; i < len(c); i++ {
		a, p := c[i-1], c[i]
		d := HaversineM(a, p)
		if d < askMinAwayM {
			continue
		}
		j := i
		for j < len(c) && j-i < seaMaxPoints && HaversineM(a, c[j]) > returnFrac*d {
			j++
		}
		var back *TrackPoint
		switch {
		case j == len(c): // the data ends out there
		case HaversineM(a, c[j]) <= returnFrac*d:
			back = &c[j]
		default: // still away after seaMaxPoints fixes: a real trip
			continue
		}
		if edgeOnly && back != nil {
			continue
		}
		if back != nil && abs64(back.TS-a.TS) > seaMaxS {
			continue
		}
		far := c[i:j]
		short := len(far) <= askMaxPoints
		// at the data's edge there is nothing to come back to: which side is the odd one out is
		// only clear when the run is a few fixes and the side it left has clearly more
		if back == nil && (!short || i < 3 || i < 2*len(far)) {
			continue
		}
		q := excursion(far, &a, back, "")
		inTime := back != nil && abs64(back.TS-a.TS) <= returnMaxS
		fastEnough := q.KMH/3.6 >= boatAskMS
		switch {
		case short && (inTime && q.KMH/3.6 >= fastAskMS || back == nil && q.KMH/3.6 >= edgeFastMS):
			q.Kind = "fast"
		case (fastEnough || (short && inTime)) && ck.OnLand != nil:
			// the costly check (the land tiles along the hops) only for runs that could be a crossing
			need := seaAskM
			if !short {
				need = seaLongAskM
			}
			if w := seaOnLegs(a, far, back, ck.OnLand); w >= need {
				q.Kind, q.SeaM = "sea", w
			}
		}
		if q.Kind == "" && short && (inTime || back == nil) && ck.NearRoad != nil {
			if near, known := ck.NearRoad(q.Lat, q.Lon, offRoadM); known && !near {
				q.Kind = "offroad"
			}
		}
		if q.Kind == "" {
			continue
		}
		out = append(out, normalise(q))
		if back != nil {
			i = j // past the run; the fix back is the next hop's start
		} else {
			break
		}
	}
	return out
}

// seaOnLegs is the longest open-water stretch on the hop out (from a to the run's first fix) or the
// hop back (from its last fix to back); 0 when the land is not known.
func seaOnLegs(a TrackPoint, far []TrackPoint, back *TrackPoint, onLand func(lat, lon float64) (bool, bool)) float64 {
	if onLand == nil || len(far) == 0 {
		return 0
	}
	w, _ := WaterRunM(a, far[0], onLand)
	if back != nil {
		if w2, _ := WaterRunM(far[len(far)-1], *back, onLand); w2 > w {
			w = w2
		}
	}
	return w
}

// WaterRunM is the longest run of open water, in metres, on the straight line from a to b, sampled
// every seaStepM (at most 400 samples); known is false when any sample's land is unknown.
func WaterRunM(a, b TrackPoint, onLand func(lat, lon float64) (bool, bool)) (float64, bool) {
	d := HaversineM(a, b)
	n := int(math.Ceil(d / seaStepM))
	if n < 2 {
		n = 2
	}
	if n > 400 {
		n = 400
	}
	step := d / float64(n)
	best, run := 0.0, 0.0
	for k := 0; k <= n; k++ {
		f := float64(k) / float64(n)
		land, known := onLand(a.Lat+(b.Lat-a.Lat)*f, a.Lon+(b.Lon-a.Lon)*f)
		if !known {
			return 0, false
		}
		if land {
			run = 0
			continue
		}
		if k > 0 {
			run += step
		}
		if run > best {
			best = run
		}
	}
	return best, true
}

// normalise puts a question's fixes in time order (a run found on the reversed points is backwards).
func normalise(q Question) Question {
	sort.Slice(q.TS, func(i, j int) bool { return q.TS[i] < q.TS[j] })
	if len(q.TS) > 0 {
		q.From, q.To = q.TS[0], q.TS[len(q.TS)-1]
	}
	return q
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// excursion describes the points far, left from `from` and come back to at `back` (either may
// be nil at the edge of the data).
func excursion(far []TrackPoint, from, back *TrackPoint, kind string) Question {
	q := Question{From: far[0].TS, To: far[len(far)-1].TS, Kind: kind}
	anchor := from
	if anchor == nil {
		anchor = back
	}
	best := -1.0
	for _, f := range far {
		q.TS = append(q.TS, f.TS)
		if d := HaversineM(*anchor, f); d > best {
			best = d
			q.Lat, q.Lon = f.Lat, f.Lon
		}
	}
	q.AwayM = best
	if from != nil && back != nil {
		q.GoneS = abs64(back.TS - from.TS)
	}
	legKMH := func(a, b TrackPoint) float64 {
		dt := float64(abs64(b.TS - a.TS))
		if dt <= 0 {
			dt = 1
		}
		return HaversineM(a, b) / dt * 3.6
	}
	if from != nil {
		q.KMH = legKMH(*from, far[0])
	}
	if back != nil {
		if v := legKMH(far[len(far)-1], *back); v > q.KMH {
			q.KMH = v
		}
	}
	return q
}
