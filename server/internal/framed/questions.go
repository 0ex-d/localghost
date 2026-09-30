package framed

// QUESTIONS ABOUT THE TRAIL. The glitch rules (clean.go) hide what cannot be true; a stretch that
// only looks unlikely is drawn, and the person is the one who knows. So a day carries questions:
// "at 14:10 the trail goes to Igoumenitsa, 38 km away, and is back 12 minutes later. Were you
// there?" No deletes those points from the database for good; yes keeps them and the stretch is
// never asked about again (trail_kept). Three kinds:
//
//   glitch   , points the glitch rules already leave off the map. Asked so the person can delete
//              them for good; nothing changes on the map either way.
//   fast     , an out-and-back that is drawn but needs more than fastAskMS (200 km/h, 50 km in 15
//              minutes) on the way out or back: a train or a flight does not come straight back.
//   offroad  , an out-and-back to a place with no road near it (the box's road tiles know the
//              area, and there is none within offRoadM), where the neighbours were somewhere the
//              phone could get to: the sea off an island, a mountainside off every path.
//
// Pure: the caller passes the road check and the decisions already made.

import (
	"sort"
)

const (
	fastAskMS    = 200.0 / 3.6 // 200 km/h
	askMinAwayM  = 5000.0      // an out-and-back shorter than this is never asked about
	askGlitchM   = 2000.0      // a glitch closer than this is not worth a question
	askMaxPoints = 3           // how many points out there make one excursion
	offRoadM     = 1000.0      // "no road near it"
)

// Question is one stretch of a day's trail the person is asked about.
type Question struct {
	From  int64   `json:"from"` // the first point asked about (unix seconds)
	To    int64   `json:"to"`   // the last
	TS    []int64 `json:"ts"`   // every point asked about: what a "no" deletes
	Lat   float64 `json:"lat"`  // the far point
	Lon   float64 `json:"lon"`
	AwayM float64 `json:"awayM"` // how far it is from where the trail left
	GoneS int64   `json:"goneS"` // from leaving to being back (0 when the day ends out there)
	KMH   float64 `json:"kmh"`   // the faster of the two legs
	Kind  string  `json:"kind"`  // glitch | fast | offroad
	Place string  `json:"place,omitempty"`
}

// TrailQuestions finds the stretches of the day [dayStart, dayEnd) worth asking about in raw (the
// day's stored points, with a couple of hours either side for context). nearRoad answers whether
// a road passes within offRoadM of a point, and whether it can tell (nil: never offroad). kept
// says whether the person already said yes to a stretch that overlaps [from, to].
func TrailQuestions(raw []TrackPoint, dayStart, dayEnd int64,
	nearRoad func(lat, lon, withinM float64) (near, known bool), kept func(from, to int64) bool) []Question {
	if len(raw) < 3 {
		return nil
	}
	pts := make([]TrackPoint, len(raw))
	copy(pts, raw)
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].TS < pts[j].TS })
	cleaned, _ := CleanTrack(pts)
	type key struct {
		ts       int64
		lat, lon float64
	}
	survived := make(map[key]int, len(cleaned))
	for _, p := range cleaned {
		survived[key{p.TS, p.Lat, p.Lon}]++
	}
	var out []Question
	add := func(q Question) {
		if q.From < dayStart || q.From >= dayEnd {
			return
		}
		if kept != nil && kept(q.From, q.To) {
			return
		}
		out = append(out, q)
	}

	// 1. the glitch rules' drops, as runs between the points that survived
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
		anchor := from
		if anchor == nil {
			anchor = back
		}
		if anchor != nil {
			q := excursion(pts[i:j], from, back, "glitch")
			if q.AwayM >= askGlitchM {
				add(q)
			}
		}
		i = j
	}

	// 2 and 3. out-and-backs among the points that are drawn
	for i := 1; i+1 < len(cleaned); i++ {
		a := cleaned[i-1]
		for n := 1; n <= askMaxPoints && i+n < len(cleaned); n++ {
			far := cleaned[i : i+n]
			b := cleaned[i+n]
			away := 0.0
			all := true
			for _, f := range far {
				d := HaversineM(a, f)
				if d < askMinAwayM {
					all = false
					break
				}
				if d > away {
					away = d
				}
			}
			if !all {
				break // a nearer point in between: not an out-and-back of this length
			}
			if HaversineM(a, b) > returnFrac*away || b.TS-a.TS > returnMaxS {
				continue // not back yet: look one point further
			}
			q := excursion(far, &a, &b, "")
			switch {
			case q.KMH/3.6 >= fastAskMS:
				q.Kind = "fast"
			case nearRoad != nil:
				fp := TrackPoint{Lat: q.Lat, Lon: q.Lon}
				if near, known := nearRoad(fp.Lat, fp.Lon, offRoadM); known && !near {
					q.Kind = "offroad"
				}
			}
			if q.Kind != "" {
				add(q)
				i += n - 1
			}
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].From < out[j].From })
	return out
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
		q.GoneS = back.TS - from.TS
	}
	legKMH := func(a, b TrackPoint) float64 {
		dt := float64(b.TS - a.TS)
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
