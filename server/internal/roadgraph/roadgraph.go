// Package roadgraph walks the streets between two points on the routing graph internal/roadtiles
// writes beside the tiles (graph/<x>_<y>.lgg): the box's own router, for the day route, where two
// fixes a quarter of an hour apart become the pavement between them rather than a chord across
// the houses. Standard library only; loads the few cells around the two points, snaps each to the
// nearest walkable road, runs A* on edge lengths, and gives back the path along the roads' own
// geometry. Nothing here is precise to the metre , OSM's geometry and a phone's fixes are not ,
// but it is the difference between a line through the sea and the road along the shore.
package roadgraph

import (
	"container/heap"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/LocalGhostDao/localghost/server/internal/roadtiles"
)

// Point is a position in degrees.
type Point struct {
	Lat, Lon float64
}

// Route is a path along the roads.
type Route struct {
	Path   []Point // from the first point's snap to the second's, along the roads
	Meters float64 // along Path
	SnapA  float64 // how far the first point was from the road it was put on, metres
	SnapB  float64
}

// Limits.
const (
	MaxWalkMeters = 8000 // beyond this a chord is drawn: nobody walks it in fifteen minutes
	SnapMeters    = 120  // a point farther than this from any road is off the roads (a beach, a boat)
	maxCells      = 64   // cells loaded for one query
	keepCells     = 400  // cells kept in memory before the graph is dropped and reloaded
)

// Errors a caller draws a chord on.
var (
	ErrTooFar  = errors.New("roadgraph: too far to walk")
	ErrOffRoad = errors.New("roadgraph: no road near a point")
	ErrNoRoute = errors.New("roadgraph: no road between the points")
	ErrNoTiles = errors.New("roadgraph: no graph on this box")
)

type node struct {
	lat, lon float64
	out      []int32 // edge indices, both directions (walking ignores one-way)
}

type edgeKey struct {
	from, to int64
	class    uint8
	meters   uint32
	n        uint16
}

// Graph is the loaded part of the road graph.
type Graph struct {
	dir    string
	mu     sync.Mutex
	loaded map[roadtiles.Cell]bool
	seen   map[edgeKey]bool
	nodes  map[int64]*node
	edges  []roadtiles.Edge
}

// Open points at a road tiles directory (the one with index.bin; graph/ beside it).
func Open(dir string) *Graph {
	g := &Graph{dir: dir}
	g.reset()
	return g
}

// Reset forgets every loaded cell: call it after the tiles were rebuilt.
func (g *Graph) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reset()
}

func (g *Graph) reset() {
	g.loaded = map[roadtiles.Cell]bool{}
	g.seen = map[edgeKey]bool{}
	g.nodes = map[int64]*node{}
	g.edges = g.edges[:0]
}

// Available reports whether the box has a graph at all.
func (g *Graph) Available() bool {
	fi, err := os.Stat(filepath.Join(g.dir, "graph"))
	return err == nil && fi.IsDir()
}

// Walkable is the class filter for a person on foot.
func Walkable(class uint8) bool { return class >= roadtiles.ClassPrimary }

// Walk is the path along the roads from a to b, or an error the caller draws a chord on.
func (g *Graph) Walk(a, b Point) (Route, error) {
	chord := roadtiles.Haversine(a.Lat, a.Lon, b.Lat, b.Lon)
	if chord > MaxWalkMeters {
		return Route{}, ErrTooFar
	}
	if !g.Available() {
		return Route{}, ErrNoTiles
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// the cells around both points, with room for a detour
	margin := math.Max(300, 0.35*chord) / 111000 // degrees of latitude
	latLo, latHi := math.Min(a.Lat, b.Lat)-margin, math.Max(a.Lat, b.Lat)+margin
	lonM := margin / math.Max(0.2, math.Cos((a.Lat+b.Lat)/2*math.Pi/180))
	lonLo, lonHi := math.Min(a.Lon, b.Lon)-lonM, math.Max(a.Lon, b.Lon)+lonM
	c0 := roadtiles.CellAt(0, lonLo, latLo)
	c1 := roadtiles.CellAt(0, lonHi, latHi)
	if (c1.X-c0.X+1)*(c1.Y-c0.Y+1) > maxCells {
		return Route{}, ErrTooFar
	}
	if len(g.loaded) > keepCells {
		g.reset()
	}
	for y := c0.Y; y <= c1.Y; y++ {
		for x := c0.X; x <= c1.X; x++ {
			if err := g.load(roadtiles.Cell{Level: 0, X: x, Y: y}); err != nil {
				return Route{}, err
			}
		}
	}
	if len(g.edges) == 0 {
		return Route{}, ErrOffRoad
	}
	sa, ok := g.snap(a)
	if !ok {
		return Route{}, ErrOffRoad
	}
	sb, ok := g.snap(b)
	if !ok {
		return Route{}, ErrOffRoad
	}
	r := Route{SnapA: sa.dist, SnapB: sb.dist}
	if sa.edge == sb.edge {
		// both on one road: along it
		r.Path = g.along(sa, sb)
		r.Meters = pathMeters(r.Path)
		return r, nil
	}
	path, err := g.astar(sa, sb, b)
	if err != nil {
		return Route{}, err
	}
	r.Path = path
	r.Meters = pathMeters(path)
	return r, nil
}

// NearRoad reports whether a road of any kind, paths included, passes within withinM of the
// point, and whether the box can tell at all: known is false without road tiles, or when none of
// the nine cells around the point holds a road (an area never cut, or the open sea). A trail
// question asks "no road goes there" only when known.
func (g *Graph) NearRoad(lat, lon, withinM float64) (near, known bool) {
	if !g.Available() {
		return false, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.loaded) > keepCells {
		g.reset()
	}
	c := roadtiles.CellAt(0, lon, lat)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			_ = g.load(roadtiles.Cell{Level: 0, X: c.X + dx, Y: c.Y + dy})
		}
	}
	span := 1.5 * roadtiles.CellDeg(0) // the nine cells, in degrees from the point
	kx := 111320 * math.Cos(lat*math.Pi/180)
	const ky = 110540.0
	best := math.Inf(1)
	for _, e := range g.edges {
		for s := 0; s+1 < len(e.Lat); s++ {
			alat, alon := float64(e.Lat[s])/1e7, float64(e.Lon[s])/1e7
			if math.Abs(alat-lat) > span || math.Abs(alon-lon) > span {
				continue
			}
			known = true
			ax, ay := (alon-lon)*kx, (alat-lat)*ky
			bx, by := (float64(e.Lon[s+1])/1e7-lon)*kx, (float64(e.Lat[s+1])/1e7-lat)*ky
			dx, dy := bx-ax, by-ay
			t := 0.0
			if l2 := dx*dx + dy*dy; l2 > 0 {
				t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l2))
			}
			if d := math.Hypot(ax+t*dx, ay+t*dy); d < best {
				best = d
			}
		}
	}
	return known && best <= withinM, known
}

// load reads one cell's edges into the graph, once.
func (g *Graph) load(c roadtiles.Cell) error {
	if g.loaded[c] {
		return nil
	}
	g.loaded[c] = true
	b, err := os.ReadFile(filepath.Join(g.dir, roadtiles.GraphName(c)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	_, edges, err := roadtiles.DecodeGraph(b)
	if err != nil {
		return err
	}
	for _, e := range edges {
		if len(e.Lat) < 2 {
			continue
		}
		k := edgeKey{e.From, e.To, e.Class, e.Meters, uint16(len(e.Lat))}
		if g.seen[k] {
			continue
		}
		g.seen[k] = true
		idx := int32(len(g.edges))
		g.edges = append(g.edges, e)
		last := len(e.Lat) - 1
		nf := g.touch(e.From, e.Lat[0], e.Lon[0])
		nf.out = append(nf.out, idx)
		nt := g.touch(e.To, e.Lat[last], e.Lon[last])
		nt.out = append(nt.out, idx)
	}
	return nil
}

func (g *Graph) touch(id int64, lat, lon int32) *node {
	n := g.nodes[id]
	if n == nil {
		n = &node{lat: float64(lat) / 1e7, lon: float64(lon) / 1e7}
		g.nodes[id] = n
	}
	return n
}

// snapped is a point put on a road: the edge, the segment (i → i+1), where along it, the position.
type snapped struct {
	edge int32
	seg  int
	t    float64
	pt   Point
	dist float64
}

// snap finds the nearest walkable road segment to p within SnapMeters.
func (g *Graph) snap(p Point) (snapped, bool) {
	best := snapped{edge: -1, dist: math.Inf(1)}
	// a flat local frame is fine at 120 m
	kx := 111320 * math.Cos(p.Lat*math.Pi/180)
	const ky = 110540.0
	for i, e := range g.edges {
		if !Walkable(e.Class) {
			continue
		}
		for s := 0; s+1 < len(e.Lat); s++ {
			ax, ay := (float64(e.Lon[s])/1e7-p.Lon)*kx, (float64(e.Lat[s])/1e7-p.Lat)*ky
			bx, by := (float64(e.Lon[s+1])/1e7-p.Lon)*kx, (float64(e.Lat[s+1])/1e7-p.Lat)*ky
			// a quick reject: the segment's box, grown by the best distance so far, misses p
			if d := best.dist; math.Min(ax, bx) > d || math.Max(ax, bx) < -d || math.Min(ay, by) > d || math.Max(ay, by) < -d {
				continue
			}
			dx, dy := bx-ax, by-ay
			l2 := dx*dx + dy*dy
			t := 0.0
			if l2 > 0 {
				t = -(ax*dx + ay*dy) / l2
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
			}
			qx, qy := ax+t*dx, ay+t*dy
			d := math.Hypot(qx, qy)
			if d < best.dist {
				best = snapped{edge: int32(i), seg: s, t: t, dist: d,
					pt: Point{Lat: p.Lat + qy/ky, Lon: p.Lon + qx/kx}}
			}
		}
	}
	return best, best.edge >= 0 && best.dist <= SnapMeters
}

// along is the path between two snaps on the same edge.
func (g *Graph) along(a, b snapped) []Point {
	e := g.edges[a.edge]
	if a.seg > b.seg || (a.seg == b.seg && a.t > b.t) {
		a, b = b, a
		p := g.along(a, b)
		for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
			p[i], p[j] = p[j], p[i]
		}
		return p
	}
	out := []Point{a.pt}
	for i := a.seg + 1; i <= b.seg; i++ {
		out = append(out, Point{float64(e.Lat[i]) / 1e7, float64(e.Lon[i]) / 1e7})
	}
	return append(out, b.pt)
}

// toEnd is the geometry from a snap to one end of its edge (toFrom: backwards to From) and its length.
func (g *Graph) toEnd(s snapped, toFrom bool) ([]Point, float64) {
	e := g.edges[s.edge]
	out := []Point{s.pt}
	if toFrom {
		for i := s.seg; i >= 0; i-- {
			out = append(out, Point{float64(e.Lat[i]) / 1e7, float64(e.Lon[i]) / 1e7})
		}
	} else {
		for i := s.seg + 1; i < len(e.Lat); i++ {
			out = append(out, Point{float64(e.Lat[i]) / 1e7, float64(e.Lon[i]) / 1e7})
		}
	}
	return out, pathMeters(out)
}

// geometry is an edge's points in the direction of travel.
func (g *Graph) geometry(idx int32, fromID int64) []Point {
	e := g.edges[idx]
	n := len(e.Lat)
	out := make([]Point, n)
	for i := 0; i < n; i++ {
		j := i
		if fromID != e.From {
			j = n - 1 - i
		}
		out[i] = Point{float64(e.Lat[j]) / 1e7, float64(e.Lon[j]) / 1e7}
	}
	return out
}

// --- A* over the junction nodes, with the two snaps as virtual ends ---

const (
	startID int64 = -1
	goalID  int64 = -2
)

type item struct {
	id  int64
	f   float64
	idx int
}
type pq []*item

func (q pq) Len() int           { return len(q) }
func (q pq) Less(i, j int) bool { return q[i].f < q[j].f }
func (q pq) Swap(i, j int)      { q[i], q[j] = q[j], q[i]; q[i].idx = i; q[j].idx = j }
func (q *pq) Push(x any)        { it := x.(*item); it.idx = len(*q); *q = append(*q, it) }
func (q *pq) Pop() any          { old := *q; it := old[len(old)-1]; *q = old[:len(old)-1]; return it }

type via struct {
	prev int64
	edge int32 // -1 for the virtual hops
}

func (g *Graph) astar(sa, sb snapped, goal Point) ([]Point, error) {
	ea, eb := g.edges[sa.edge], g.edges[sb.edge]
	dist := map[int64]float64{startID: 0}
	from := map[int64]via{}
	done := map[int64]bool{}
	h := func(id int64) float64 {
		if id == goalID {
			return 0
		}
		n := g.nodes[id]
		return roadtiles.Haversine(n.lat, n.lon, goal.Lat, goal.Lon)
	}
	q := &pq{}
	heap.Push(q, &item{id: startID, f: 0})
	relax := func(u, v int64, cost float64, edge int32) {
		nd := dist[u] + cost
		if d, ok := dist[v]; ok && d <= nd {
			return
		}
		dist[v] = nd
		from[v] = via{prev: u, edge: edge}
		heap.Push(q, &item{id: v, f: nd + h(v)})
	}
	// the goal edge's ends reach the goal along the edge
	_, gFrom := g.toEnd(sb, true)
	_, gTo := g.toEnd(sb, false)
	for q.Len() > 0 {
		it := heap.Pop(q).(*item)
		u := it.id
		if done[u] {
			continue
		}
		done[u] = true
		if u == goalID {
			break
		}
		if u == startID {
			_, a := g.toEnd(sa, true)
			_, b := g.toEnd(sa, false)
			relax(startID, ea.From, a, -1)
			relax(startID, ea.To, b, -1)
			continue
		}
		if u == eb.From {
			relax(u, goalID, gFrom, -1)
		}
		if u == eb.To {
			relax(u, goalID, gTo, -1)
		}
		n := g.nodes[u]
		if n == nil {
			continue
		}
		for _, ei := range n.out {
			e := &g.edges[ei]
			if !Walkable(e.Class) {
				continue
			}
			v := e.To
			if u == e.To {
				v = e.From
			}
			relax(u, v, math.Max(1, float64(e.Meters)), ei)
		}
	}
	if !done[goalID] {
		return nil, ErrNoRoute
	}
	// walk back
	var hops []via
	var ids []int64
	for id := goalID; id != startID; id = from[id].prev {
		hops = append(hops, from[id])
		ids = append(ids, id)
	}
	// hops[len-1] is start→first junction; hops[0] is last junction→goal
	var path []Point
	for i := len(hops) - 1; i >= 0; i-- {
		hop, to := hops[i], ids[i]
		switch {
		case hop.prev == startID:
			p, _ := g.toEnd(sa, to == ea.From)
			path = append(path, p...)
		case to == goalID:
			p, _ := g.toEnd(sb, hop.prev == eb.From)
			// p runs goal → end; we need end → goal
			for k, j := 0, len(p)-1; k < j; k, j = k+1, j-1 {
				p[k], p[j] = p[j], p[k]
			}
			path = append(path, p[1:]...)
		default:
			p := g.geometry(hop.edge, hop.prev)
			path = append(path, p[1:]...)
		}
	}
	return path, nil
}

func pathMeters(p []Point) float64 {
	var m float64
	for i := 1; i < len(p); i++ {
		m += roadtiles.Haversine(p[i-1].Lat, p[i-1].Lon, p[i].Lat, p[i].Lon)
	}
	return m
}
