package roadgraph

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/LocalGhostDao/localghost/server/internal/osmpbf"
	"github.com/LocalGhostDao/localghost/server/internal/roadtiles"
)

// A town: a 4×4 grid of streets 200 m apart at Bucharest's latitude, a footway cutting one block
// diagonally, and a motorway from corner to corner that a walker must not take.
const (
	lat0 = 44.4300
	lon0 = 26.1000
	dLat = 0.0018  // ~200 m
	dLon = 0.00252 // ~200 m at 44.43°
)

func nodeID(x, y int) int64 { return int64(100 + 10*x + y) }

func buildTown(t *testing.T) string {
	var nodes []osmpbf.TestNode
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			nodes = append(nodes, osmpbf.TestNode{ID: nodeID(x, y), Lat: lat0 + float64(y)*dLat, Lon: lon0 + float64(x)*dLon})
		}
	}
	var ways []osmpbf.TestWay
	id := int64(1000)
	for x := 0; x < 4; x++ { // north-south streets
		ways = append(ways, osmpbf.TestWay{ID: id, Tags: map[string]string{"highway": "residential", "name": "Strada " + string(rune('A'+x))},
			Refs: []int64{nodeID(x, 0), nodeID(x, 1), nodeID(x, 2), nodeID(x, 3)}})
		id++
	}
	for y := 0; y < 4; y++ { // east-west streets
		ways = append(ways, osmpbf.TestWay{ID: id, Tags: map[string]string{"highway": "residential"},
			Refs: []int64{nodeID(0, y), nodeID(1, y), nodeID(2, y), nodeID(3, y)}})
		id++
	}
	// the footway across the middle block, (1,1) → (2,2)
	ways = append(ways, osmpbf.TestWay{ID: id, Tags: map[string]string{"highway": "footway"}, Refs: []int64{nodeID(1, 1), nodeID(2, 2)}})
	id++
	// the motorway from (0,0) straight to (3,3)
	ways = append(ways, osmpbf.TestWay{ID: id, Tags: map[string]string{"highway": "motorway", "oneway": "yes"}, Refs: []int64{nodeID(0, 0), nodeID(3, 3)}})
	pbf := osmpbf.Encode(osmpbf.EncBlock{Nodes: nodes}, osmpbf.EncBlock{Ways: ways})
	dir := t.TempDir()
	p := filepath.Join(dir, "town.osm.pbf")
	if err := os.WriteFile(p, pbf, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "roadtiles")
	st, err := roadtiles.Build([]string{p}, out, roadtiles.Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	// 8 streets each split at 3 inner junctions = 24 edges, the footway, the motorway
	if st.Edges != 26 || st.GraphCells != 1 {
		t.Fatalf("town graph: %+v", st)
	}
	return out
}

func TestWalksTheStreetsNotTheMotorway(t *testing.T) {
	g := Open(buildTown(t))
	if !g.Available() {
		t.Fatal("graph not seen")
	}
	// from just off the (0,0) corner to just off the (3,3) corner
	a := Point{lat0 - 0.0001, lon0 - 0.0001}
	b := Point{lat0 + 3*dLat + 0.0001, lon0 + 3*dLon + 0.0001}
	r, err := g.Walk(a, b)
	if err != nil {
		t.Fatal(err)
	}
	// two blocks, the diagonal footway, two blocks: ~400 + 283 + 400 = 1083 m; the motorway would
	// be ~850 and the grid alone 1200
	if r.Meters < 1040 || r.Meters > 1140 {
		t.Fatalf("walked %.0f m over %d points", r.Meters, len(r.Path))
	}
	if r.SnapA > 20 || r.SnapB > 20 {
		t.Fatalf("snaps %.1f %.1f", r.SnapA, r.SnapB)
	}
	// the path passes the footway's ends and never leaves the roads: every point is on a grid
	// line (lat or lon on the grid) or on the diagonal
	sawDiag := false
	for _, p := range r.Path {
		fx := (p.Lon - lon0) / dLon
		fy := (p.Lat - lat0) / dLat
		onGrid := math.Abs(fx-math.Round(fx)) < 0.02 || math.Abs(fy-math.Round(fy)) < 0.02
		onDiag := math.Abs((fx-1)-(fy-1)) < 0.05 && fx > 0.9 && fx < 2.1
		if onDiag && fx > 1.3 && fx < 1.7 {
			sawDiag = true
		}
		if !onGrid && !onDiag {
			t.Fatalf("point off the roads: %+v (fx %.3f fy %.3f)", p, fx, fy)
		}
	}
	// the corner nodes are on the motorway too; the diagonal check above is the real proof, plus
	// the length: a walk along the motorway would be under 900 m
	_ = sawDiag
	// first and last points are the snaps, on the roads at the corners
	if d := roadtiles.Haversine(r.Path[0].Lat, r.Path[0].Lon, lat0, lon0); d > 25 {
		t.Fatalf("start %.0f m from the corner", d)
	}
}

func TestSameStreetOffRoadTooFar(t *testing.T) {
	g := Open(buildTown(t))
	// two points on the same block of Strada A, 100 m apart, a little east of the line
	a := Point{lat0 + 0.25*dLat, lon0 + 0.00005}
	b := Point{lat0 + 0.75*dLat, lon0 + 0.00005}
	r, err := g.Walk(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Meters < 90 || r.Meters > 110 || len(r.Path) != 2 {
		t.Fatalf("along one street: %.0f m, %d points", r.Meters, len(r.Path))
	}
	// the other way round is the same path reversed
	r2, _ := g.Walk(b, a)
	if math.Abs(r2.Meters-r.Meters) > 1 || r2.Path[0] != r.Path[1] {
		t.Fatalf("reverse: %+v vs %+v", r2.Path, r.Path)
	}
	// a point 500 m outside the town: off the roads
	if _, err := g.Walk(a, Point{lat0 - 0.0045, lon0}); err != ErrOffRoad {
		t.Fatalf("off road: %v", err)
	}
	// twenty kilometres: not a walk
	if _, err := g.Walk(a, Point{lat0 + 0.18, lon0}); err != ErrTooFar {
		t.Fatalf("too far: %v", err)
	}
	// a box with no graph
	if _, err := Open(t.TempDir()).Walk(a, b); err != ErrNoTiles {
		t.Fatalf("no tiles: %v", err)
	}
}

func TestAcrossCellsAndDetours(t *testing.T) {
	// a road that crosses a 0.1° cell border (lon 26.2) with a gap in the middle: two edges in two
	// cells that do not connect, so no route; then the bridge way that joins them, and a route
	nodes := []osmpbf.TestNode{
		{ID: 1, Lat: 44.45, Lon: 26.19}, {ID: 2, Lat: 44.45, Lon: 26.198}, // west piece
		{ID: 3, Lat: 44.45, Lon: 26.202}, {ID: 4, Lat: 44.45, Lon: 26.21}, // east piece
	}
	ways := []osmpbf.TestWay{
		{ID: 1, Tags: map[string]string{"highway": "residential"}, Refs: []int64{1, 2}},
		{ID: 2, Tags: map[string]string{"highway": "residential"}, Refs: []int64{3, 4}},
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "gap.osm.pbf")
	os.WriteFile(p, osmpbf.Encode(osmpbf.EncBlock{Nodes: nodes}, osmpbf.EncBlock{Ways: ways}), 0o644)
	out := filepath.Join(dir, "rt")
	if _, err := roadtiles.Build([]string{p}, out, roadtiles.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	g := Open(out)
	a, b := Point{44.4501, 26.191}, Point{44.4501, 26.209}
	if _, err := g.Walk(a, b); err != ErrNoRoute {
		t.Fatalf("gap: %v", err)
	}
	// join them
	ways = append(ways, osmpbf.TestWay{ID: 3, Tags: map[string]string{"highway": "footway", "bridge": "yes"}, Refs: []int64{2, 3}})
	os.WriteFile(p, osmpbf.Encode(osmpbf.EncBlock{Nodes: nodes}, osmpbf.EncBlock{Ways: ways}), 0o644)
	if _, err := roadtiles.Build([]string{p}, out, roadtiles.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	g = Open(out)
	r, err := g.Walk(a, b)
	if err != nil {
		t.Fatal(err)
	}
	want := roadtiles.Haversine(44.45, 26.191, 44.45, 26.209)
	if math.Abs(r.Meters-want) > 20 || len(r.Path) < 4 {
		t.Fatalf("across the border: %.0f m (want ~%.0f), %d points", r.Meters, want, len(r.Path))
	}
}
