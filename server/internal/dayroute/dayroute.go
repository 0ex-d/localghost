// Package dayroute turns a day's data points into the day as a person would tell it: the places
// they stayed and the moves between them, on foot or by something faster, along the streets where
// the roads are known. The points are the quarter-hour fixes, the photos' positions (each a fix at
// its time) and, when the phone syncs it, the day's step count as a check. Pure: points in, a Day
// out; the box (ghost.framed) names the stays from GeoNames and walks the streets on the road
// graph through two small interfaces, so all of this runs in a test with neither.
//
// The rules, in plain words: a STAY is ten minutes or more within a hundred metres (the sampling
// is a fix every fifteen minutes, so two fixes in the same spot are a stay; one is not); a MOVE is
// what happens between two stays, walked when no hop between its fixes was faster than a person
// walks, ridden otherwise; a walked hop between two fixes is drawn along the roads when the router
// finds a way that is not absurdly longer than the straight line, and as the straight line when
// the fixes are off the roads (a beach, a boat) or the box has no roads there.
package dayroute

import (
	"math"
	"sort"
	"strconv"

	"github.com/LocalGhostDao/localghost/server/internal/roadgraph"
)

// Fix is one known position at one time: a location sample, or a geotagged photo.
type Fix struct {
	TS       int64
	Lat, Lon float64
	Photo    bool
}

// Namer names a stay's position: the spot it is at (a beach, a harbour, a museum) or the place it
// is near. Empty name when nothing is known. kind is a plain word ("beach", "harbour", "near").
type Namer interface {
	StayName(lat, lon float64) (name, kind string)
}

// Router walks the streets between two points; an error means "draw the chord".
type Router interface {
	Walk(a, b roadgraph.Point) (roadgraph.Route, error)
}

// Stay is time spent in one place.
type Stay struct {
	Name   string  `json:"name,omitempty"`
	Kind   string  `json:"kind,omitempty"` // beach, harbour, … or "near" (name is the nearest place)
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	From   int64   `json:"from"` // unix seconds, first fix here
	To     int64   `json:"to"`   // last fix here
	Fixes  int     `json:"fixes"`
	Photos int     `json:"photos,omitempty"`
}

// Move is the way from one stay to the next (or from the day's first fix, or to its last).
type Move struct {
	Mode   string       `json:"mode"` // "walk" or "ride"
	From   int64        `json:"from"`
	To     int64        `json:"to"`
	Meters float64      `json:"meters"`           // along Path
	ChordM float64      `json:"chordM"`           // straight lines between the fixes, for comparison
	Routed int          `json:"routed,omitempty"` // hops drawn along the roads
	Hops   int          `json:"hops"`             // hops between fixes (a hop per quarter hour, roughly)
	Path   [][2]float64 `json:"path"`             // [lat, lon]
	Photos int          `json:"photos,omitempty"`
	KmH    float64      `json:"kmh"` // the fastest hop, as the mode was judged
}

// Day is the day told as stays and moves.
type Day struct {
	Day    string  `json:"day"`
	Stays  []Stay  `json:"stays"`
	Moves  []Move  `json:"moves"`
	WalkM  float64 `json:"walkM"`
	RideM  float64 `json:"rideM"`
	Fixes  int     `json:"fixes"`
	Photos int     `json:"photos"`
	Steps  float64 `json:"steps,omitempty"` // the day's steps from the phone's health sync, when known
	Note   string  `json:"note,omitempty"`  // one line when the numbers disagree with themselves
	Line   string  `json:"line"`            // Title(): the day in one line
}

// Rules.
const (
	StayRadiusM    = 100.0 // within this of the running centre is the same place
	StayMinSeconds = 600   // ten minutes makes a stay
	WalkMaxKmH     = 7.0   // faster than this on any hop and the move was ridden
	RouteMaxRatio  = 2.5   // a street route longer than this × the chord (+100 m) is the wrong road: chord
	JitterM        = 15.0  // hops shorter than this are standing still
	StepMeters     = 0.75  // a step, to compare the walked distance with the step count
)

// Build tells the day. fixes may be in any order and may include photos; steps is 0 when unknown;
// namer and router may be nil.
func Build(day string, fixes []Fix, steps float64, namer Namer, router Router) Day {
	d := Day{Day: day, Stays: []Stay{}, Moves: []Move{}, Steps: steps}
	pts := make([]Fix, 0, len(fixes))
	for _, f := range fixes {
		if f.Lat == 0 && f.Lon == 0 {
			continue
		}
		pts = append(pts, f)
		if f.Photo {
			d.Photos++
		} else {
			d.Fixes++
		}
	}
	if len(pts) == 0 {
		return d
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].TS < pts[j].TS })
	// stays: runs of fixes within StayRadiusM of their running centre for StayMinSeconds or more
	type run struct{ i, j int } // pts[i..j] inclusive
	var stays []run
	for i := 0; i < len(pts); {
		clat, clon := pts[i].Lat, pts[i].Lon
		j := i
		for k := i + 1; k < len(pts); k++ {
			if haversine(clat, clon, pts[k].Lat, pts[k].Lon) > StayRadiusM {
				break
			}
			n := float64(k - i + 1)
			clat += (pts[k].Lat - clat) / n
			clon += (pts[k].Lon - clon) / n
			j = k
		}
		if j > i && pts[j].TS-pts[i].TS >= StayMinSeconds {
			stays = append(stays, run{i, j})
			i = j + 1
			continue
		}
		i++
	}
	// the stays themselves
	for _, r := range stays {
		s := Stay{From: pts[r.i].TS, To: pts[r.j].TS}
		var lat, lon float64
		for k := r.i; k <= r.j; k++ {
			lat += pts[k].Lat
			lon += pts[k].Lon
			if pts[k].Photo {
				s.Photos++
			} else {
				s.Fixes++
			}
		}
		n := float64(r.j - r.i + 1)
		s.Lat, s.Lon = round6(lat/n), round6(lon/n)
		if namer != nil {
			s.Name, s.Kind = namer.StayName(s.Lat, s.Lon)
		}
		d.Stays = append(d.Stays, s)
	}
	// the moves: the fixes between the end of one stay and the start of the next, anchored on the
	// stays' centres; before the first stay and after the last, the day's loose ends
	anchor := func(si int) Fix {
		s := d.Stays[si]
		return Fix{TS: 0, Lat: s.Lat, Lon: s.Lon}
	}
	segment := func(from, to int, startAnchor, endAnchor *Fix) {
		var seq []Fix
		if startAnchor != nil {
			a := *startAnchor
			a.TS = pts[from-1].TS // the stay's last fix
			seq = append(seq, a)
		}
		seq = append(seq, pts[from:to+1]...)
		if endAnchor != nil {
			z := *endAnchor
			z.TS = pts[to+1].TS // the next stay's first fix
			seq = append(seq, z)
		}
		if m, ok := buildMove(seq, router); ok {
			d.Moves = append(d.Moves, m)
		}
	}
	if len(stays) == 0 {
		segment(0, len(pts)-1, nil, nil)
	} else {
		if stays[0].i > 0 {
			a := anchor(0)
			segment(0, stays[0].i-1, nil, &a)
		}
		for k := 0; k+1 < len(stays); k++ {
			a, z := anchor(k), anchor(k+1)
			from, to := stays[k].j+1, stays[k+1].i-1
			if from > to {
				// two stays with nothing between: one hop from centre to centre
				seq := []Fix{{TS: pts[stays[k].j].TS, Lat: a.Lat, Lon: a.Lon}, {TS: pts[stays[k+1].i].TS, Lat: z.Lat, Lon: z.Lon}}
				if m, ok := buildMove(seq, router); ok {
					d.Moves = append(d.Moves, m)
				}
				continue
			}
			segment(from, to, &a, &z)
		}
		if last := stays[len(stays)-1]; last.j < len(pts)-1 {
			a := anchor(len(stays) - 1)
			segment(last.j+1, len(pts)-1, &a, nil)
		}
	}
	for _, m := range d.Moves {
		if m.Mode == "walk" {
			d.WalkM += m.Meters
		} else {
			d.RideM += m.Meters
		}
	}
	d.WalkM, d.RideM = math.Round(d.WalkM), math.Round(d.RideM)
	if steps > 0 && d.WalkM > steps*StepMeters*1.5+500 {
		d.Note = "walked farther than the steps say; some of it was probably not on foot"
	}
	d.Line = d.Title()
	return d
}

// buildMove judges and draws one move over a time-ordered sequence of fixes.
func buildMove(seq []Fix, router Router) (Move, bool) {
	if len(seq) < 2 {
		return Move{}, false
	}
	m := Move{From: seq[0].TS, To: seq[len(seq)-1].TS, Mode: "walk"}
	var maxKmH float64
	for i := 1; i < len(seq); i++ {
		a, b := seq[i-1], seq[i]
		if b.Photo {
			m.Photos++
		}
		d := haversine(a.Lat, a.Lon, b.Lat, b.Lon)
		if d < JitterM {
			continue
		}
		m.ChordM += d
		m.Hops++
		if dt := b.TS - a.TS; dt > 0 {
			if kmh := d / float64(dt) * 3.6; kmh > maxKmH {
				maxKmH = kmh
			}
		} else {
			// two fixes at the same second (a photo and a sample): distance without a speed
		}
	}
	if m.Hops == 0 {
		return Move{}, false // standing still between two stays: not a move
	}
	m.KmH = math.Round(maxKmH*10) / 10
	if maxKmH > WalkMaxKmH {
		m.Mode = "ride"
	}
	// draw: chords, or the streets for a walk
	m.Path = append(m.Path, [2]float64{round6(seq[0].Lat), round6(seq[0].Lon)})
	for i := 1; i < len(seq); i++ {
		a, b := seq[i-1], seq[i]
		d := haversine(a.Lat, a.Lon, b.Lat, b.Lon)
		if d < JitterM {
			continue
		}
		if m.Mode == "walk" && router != nil {
			r, err := router.Walk(roadgraph.Point{Lat: a.Lat, Lon: a.Lon}, roadgraph.Point{Lat: b.Lat, Lon: b.Lon})
			if err == nil && r.Meters <= RouteMaxRatio*d+100 && len(r.Path) >= 2 {
				// the fix, its snap, the streets, the next fix's snap, the next fix
				for _, p := range r.Path {
					m.Path = append(m.Path, [2]float64{round6(p.Lat), round6(p.Lon)})
				}
				m.Path = append(m.Path, [2]float64{round6(b.Lat), round6(b.Lon)})
				m.Meters += r.Meters + r.SnapA + r.SnapB
				m.Routed++
				continue
			}
		}
		m.Path = append(m.Path, [2]float64{round6(b.Lat), round6(b.Lon)})
		m.Meters += d
	}
	m.Meters = math.Round(m.Meters)
	m.ChordM = math.Round(m.ChordM)
	return m, true
}

// Title is the day in one line: "home → Gaios harbour → Voutoumi beach → home · 6.2 km on foot, 31 km by road".
func (d Day) Title() string {
	var s string
	for i, st := range d.Stays {
		name := st.Name
		if name == "" {
			name = "a stop"
		} else if st.Kind == "near" {
			name = "near " + name
		}
		if i > 0 {
			s += " → "
		}
		s += name
	}
	var dist string
	if d.WalkM >= 100 {
		dist = km(d.WalkM) + " on foot"
	}
	if d.RideM >= 1000 {
		if dist != "" {
			dist += ", "
		}
		dist += km(d.RideM) + " by road"
	}
	switch {
	case s == "" && dist == "":
		return ""
	case s == "":
		return dist
	case dist == "":
		return s
	}
	return s + " · " + dist
}

func km(m float64) string {
	if m < 10000 {
		return strconv.FormatFloat(math.Round(m/100)/10, 'f', -1, 64) + " km"
	}
	return strconv.FormatFloat(math.Round(m/1000), 'f', 0, 64) + " km"
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := p2-p1, (lon2-lon1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(a)))
}
