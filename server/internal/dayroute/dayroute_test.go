package dayroute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LocalGhostDao/localghost/server/internal/osmpbf"
	"github.com/LocalGhostDao/localghost/server/internal/roadgraph"
	"github.com/LocalGhostDao/localghost/server/internal/roadtiles"
)

// the town from roadgraph's tests: a 4×4 grid of streets 200 m apart
const (
	lat0 = 44.4300
	lon0 = 26.1000
	dLat = 0.0018
	dLon = 0.00252
)

func town(t *testing.T) *roadgraph.Graph {
	var nodes []osmpbf.TestNode
	id := func(x, y int) int64 { return int64(100 + 10*x + y) }
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			nodes = append(nodes, osmpbf.TestNode{ID: id(x, y), Lat: lat0 + float64(y)*dLat, Lon: lon0 + float64(x)*dLon})
		}
	}
	var ways []osmpbf.TestWay
	w := int64(1000)
	for x := 0; x < 4; x++ {
		ways = append(ways, osmpbf.TestWay{ID: w, Tags: map[string]string{"highway": "residential"}, Refs: []int64{id(x, 0), id(x, 1), id(x, 2), id(x, 3)}})
		w++
	}
	for y := 0; y < 4; y++ {
		ways = append(ways, osmpbf.TestWay{ID: w, Tags: map[string]string{"highway": "residential"}, Refs: []int64{id(0, y), id(1, y), id(2, y), id(3, y)}})
		w++
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "town.osm.pbf")
	os.WriteFile(p, osmpbf.Encode(osmpbf.EncBlock{Nodes: nodes}, osmpbf.EncBlock{Ways: ways}), 0o644)
	out := filepath.Join(dir, "roadtiles")
	if _, err := roadtiles.Build([]string{p}, out, roadtiles.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	return roadgraph.Open(out)
}

type namer func(lat, lon float64) (string, string)

func (n namer) StayName(lat, lon float64) (string, string) { return n(lat, lon) }

func TestADayToldAsStaysAndMoves(t *testing.T) {
	g := town(t)
	const day0 = int64(1_790_000_000) // some midnight-ish second; only differences matter
	q := int64(900)                   // a quarter hour
	var fixes []Fix
	// home: off the (0,0) corner by ~50 m, midnight to 08:00 with a little jitter
	home := Fix{Lat: lat0 - 0.0004, Lon: lon0 - 0.0003}
	for i := int64(0); i <= 32; i++ {
		j := float64(i%3) * 0.00005
		fixes = append(fixes, Fix{TS: day0 + i*q, Lat: home.Lat + j, Lon: home.Lon - j})
	}
	// a walk through the town: 08:15 at (1,1), 08:30 at (2,2), 08:45 at the (3,3) corner
	fixes = append(fixes,
		Fix{TS: day0 + 33*q, Lat: lat0 + dLat + 0.00003, Lon: lon0 + dLon - 0.00003},
		Fix{TS: day0 + 34*q, Lat: lat0 + 2*dLat - 0.00003, Lon: lon0 + 2*dLon + 0.00003},
	)
	// the café at (3,3): 08:45, 09:00, 09:15, and a photo at 09:05
	cafe := Fix{Lat: lat0 + 3*dLat + 0.0002, Lon: lon0 + 3*dLon + 0.0002}
	fixes = append(fixes,
		Fix{TS: day0 + 35*q, Lat: cafe.Lat, Lon: cafe.Lon},
		Fix{TS: day0 + 36*q, Lat: cafe.Lat + 0.00004, Lon: cafe.Lon},
		Fix{TS: day0 + 36*q + 300, Lat: cafe.Lat + 0.00002, Lon: cafe.Lon + 0.00002, Photo: true},
		Fix{TS: day0 + 37*q, Lat: cafe.Lat, Lon: cafe.Lon + 0.00004},
	)
	// a ride to the beach, 10 km east: 09:30 halfway, 09:45 there
	beach := Fix{Lat: lat0 + 0.01, Lon: lon0 + 0.126}
	fixes = append(fixes, Fix{TS: day0 + 38*q, Lat: lat0 + 0.005, Lon: lon0 + 0.065})
	for i := int64(39); i <= 48; i++ {
		fixes = append(fixes, Fix{TS: day0 + i*q, Lat: beach.Lat + float64(i%2)*0.00003, Lon: beach.Lon})
	}
	nm := namer(func(lat, lon float64) (string, string) {
		switch {
		case haversine(lat, lon, beach.Lat, beach.Lon) < 200:
			return "Voutoumi", "beach"
		case haversine(lat, lon, cafe.Lat, cafe.Lon) < 200:
			return "Corner Café", "cafe"
		}
		return "Strada A", "near"
	})
	d := Build("2026-09-25", fixes, 4000, nm, g)
	if len(d.Stays) != 3 {
		t.Fatalf("stays: %+v", d.Stays)
	}
	if d.Stays[0].Kind != "near" || d.Stays[0].Fixes != 33 || d.Stays[0].To-d.Stays[0].From != 32*q {
		t.Fatalf("home stay: %+v", d.Stays[0])
	}
	if d.Stays[1].Name != "Corner Café" || d.Stays[1].Photos != 1 || d.Stays[1].Fixes != 3 {
		t.Fatalf("café stay: %+v", d.Stays[1])
	}
	if d.Stays[2].Name != "Voutoumi" || d.Stays[2].From != day0+39*q {
		t.Fatalf("beach stay: %+v", d.Stays[2])
	}
	if len(d.Moves) != 2 {
		t.Fatalf("moves: %d", len(d.Moves))
	}
	walk, ride := d.Moves[0], d.Moves[1]
	if walk.Mode != "walk" || walk.Hops != 3 || walk.Routed != 3 || walk.KmH > 2.5 {
		t.Fatalf("walk: %+v", walk)
	}
	// along the streets the walk is longer than the diagonal chords: three blocks-ish each hop
	if walk.Meters <= walk.ChordM || walk.Meters < 1000 || walk.Meters > 1700 {
		t.Fatalf("walk %0.f m along the streets vs %0.f m of chords", walk.Meters, walk.ChordM)
	}
	// every point of the walk's path is on a street line, or is one of the fixes
	for _, p := range walk.Path {
		fx := (p[1] - lon0) / dLon
		fy := (p[0] - lat0) / dLat
		onGrid := abs(fx-round(fx)) < 0.03 || abs(fy-round(fy)) < 0.03
		isFix := false
		for _, f := range fixes {
			if haversine(p[0], p[1], f.Lat, f.Lon) < 1 {
				isFix = true
			}
		}
		if !onGrid && !isFix {
			t.Fatalf("walk point off the streets: %v", p)
		}
	}
	if ride.Mode != "ride" || ride.KmH < 15 || ride.Routed != 0 || ride.Hops != 2 || ride.Meters < 9000 {
		t.Fatalf("ride: %+v", ride)
	}
	if d.WalkM != walk.Meters || d.RideM != ride.Meters || d.Fixes != 49 || d.Photos != 1 || d.Note != "" {
		t.Fatalf("day: walk %v ride %v fixes %d photos %d note %q", d.WalkM, d.RideM, d.Fixes, d.Photos, d.Note)
	}
	if got := d.Title(); got != "near Strada A → Corner Café → Voutoumi · 1.4 km on foot, 10 km by road" && !strings.HasPrefix(got, "near Strada A → Corner Café → Voutoumi · 1.") {
		t.Fatalf("title: %q", got)
	}
	// the step check: 500 steps cannot walk this
	if d2 := Build("2026-09-25", fixes, 500, nm, g); d2.Note == "" {
		t.Fatal("no note on too few steps")
	}
	// it serialises to what the phone reads
	b, _ := json.Marshal(d)
	var back Day
	if err := json.Unmarshal(b, &back); err != nil || len(back.Moves[0].Path) != len(walk.Path) {
		t.Fatal("json round trip")
	}
}

func TestEdges(t *testing.T) {
	if d := Build("2026-01-01", nil, 0, nil, nil); len(d.Stays) != 0 || len(d.Moves) != 0 || d.Title() != "" {
		t.Fatalf("empty: %+v", d)
	}
	// one fix: nothing to tell
	if d := Build("2026-01-01", []Fix{{TS: 1, Lat: 44.4, Lon: 26.1}}, 0, nil, nil); len(d.Stays)+len(d.Moves) != 0 {
		t.Fatalf("one fix: %+v", d)
	}
	// two fixes 5 m apart: standing still, no move; and too short for a stay (five minutes)
	if d := Build("2026-01-01", []Fix{{TS: 0, Lat: 44.4, Lon: 26.1}, {TS: 300, Lat: 44.40004, Lon: 26.1}}, 0, nil, nil); len(d.Stays)+len(d.Moves) != 0 {
		t.Fatalf("jitter: %+v", d)
	}
	// two fixes 5 m apart ten minutes on: a stay, still no move
	if d := Build("2026-01-01", []Fix{{TS: 0, Lat: 44.4, Lon: 26.1}, {TS: 600, Lat: 44.40004, Lon: 26.1}}, 0, nil, nil); len(d.Stays) != 1 || len(d.Moves) != 0 {
		t.Fatalf("stay: %+v", d)
	}
	// no stays at all: the whole day is one move, no router: chords, judged by speed
	fixes := []Fix{{TS: 0, Lat: 44.40, Lon: 26.10}, {TS: 900, Lat: 44.41, Lon: 26.10}, {TS: 1800, Lat: 44.42, Lon: 26.10}}
	d := Build("2026-01-01", fixes, 0, nil, nil)
	if len(d.Moves) != 1 || d.Moves[0].Mode != "walk" || d.Moves[0].Hops != 2 || len(d.Moves[0].Path) != 3 || d.Moves[0].Routed != 0 {
		t.Fatalf("one move: %+v", d.Moves)
	}
	if d.Title() != "2.2 km on foot" {
		t.Fatalf("title %q", d.Title())
	}
	// the same in five minutes: a ride
	fixes[1].TS, fixes[2].TS = 150, 300
	if d := Build("2026-01-01", fixes, 0, nil, nil); d.Moves[0].Mode != "ride" || d.Title() != "2.2 km by road" {
		t.Fatalf("ride: %+v %q", d.Moves, d.Title())
	}
	// photos alone make a day
	ph := []Fix{{TS: 0, Lat: 44.40, Lon: 26.10, Photo: true}, {TS: 1200, Lat: 44.40001, Lon: 26.10, Photo: true}}
	if d := Build("2026-01-01", ph, 0, nil, nil); len(d.Stays) != 1 || d.Stays[0].Photos != 2 || d.Photos != 2 || d.Fixes != 0 {
		t.Fatalf("photos: %+v", d)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func round(v float64) float64 {
	if v < 0 {
		return float64(int64(v - 0.5))
	}
	return float64(int64(v + 0.5))
}
