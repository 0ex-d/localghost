package tzgrid

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata"
)

// Two zones: a square "Europe/Testland" with a hole that belongs to "Europe/Enclave", and an
// ocean zone around them. The grid names the zone at a point, the hole included.
const world = `{"type":"FeatureCollection","features":[
{"type":"Feature","properties":{"tzid":"Etc/GMT"},"geometry":{"type":"Polygon","coordinates":[[[-10,30],[30,30],[30,60],[-10,60],[-10,30]],[[10.02,40.02],[12.02,40.02],[12.02,42.02],[10.02,42.02],[10.02,40.02]]]}},
{"type":"Feature","properties":{"tzid":"Europe/Testland"},"geometry":{"type":"MultiPolygon","coordinates":[[[[10.02,40.02],[12.02,40.02],[12.02,42.02],[10.02,42.02],[10.02,40.02]],[[10.52,40.52],[11.02,40.52],[11.02,41.02],[10.52,41.02],[10.52,40.52]]]]}},
{"type":"Feature","properties":{"tzid":"Europe/Enclave"},"geometry":{"type":"Polygon","coordinates":[[[10.52,40.52],[11.02,40.52],[11.02,41.02],[10.52,41.02],[10.52,40.52]]]}},
{"type":"Feature","properties":{},"geometry":{"type":"Point","coordinates":[0,0]}}
]}`

func TestBuildAndLookup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "combined-with-oceans.json")
	os.WriteFile(src, []byte(world), 0o644)
	grid := filepath.Join(dir, "grid.bin")
	if !Stale(src, grid) {
		t.Fatal("no grid yet must be stale")
	}
	n, err := Build(src, grid, nil)
	if err != nil || n != 3 {
		t.Fatalf("build: %d %v", n, err)
	}
	if Stale(src, grid) {
		t.Fatal("a fresh grid is not stale")
	}
	l, err := Open(grid)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if z := l.Zone(41.5, 11.5); z != "Europe/Testland" {
		t.Fatalf("inside: %q", z)
	}
	if z := l.Zone(40.75, 10.75); z != "Europe/Enclave" {
		t.Fatalf("the hole: %q", z)
	}
	if z := l.Zone(50, 0); z != "Etc/GMT" {
		t.Fatalf("the ocean: %q", z)
	}
	if z := l.Zone(-45, 100); z != "" {
		t.Fatalf("outside everything: %q", z)
	}
	if z := l.Zone(91, 0); z != "" {
		t.Fatal("off the globe answered")
	}
	if len(l.Zones()) != 3 || FindGeoJSON(dir) != src {
		t.Fatalf("zones %v find %q", l.Zones(), FindGeoJSON(dir))
	}
	if Location("Europe/Athens") == nil || Location("Nowhere/Nothing") != nil || Location("") != nil {
		t.Fatal("Location")
	}
	// a 19:00 in Athens is not a 19:00 in London
	at := time.Date(2026, 10, 1, 19, 0, 0, 0, Location("Europe/Athens"))
	if at.UTC().Hour() != 16 {
		t.Fatalf("athens 19:00 is %d UTC", at.UTC().Hour())
	}
}
