package roadtiles

// The routing graph, cut beside the tiles from the same PBFs. The tiles are for drawing: pieces
// with no notion of which road meets which. A route along the streets needs the topology, so the
// build also writes EDGES: a way, split at every node it shares with another road (a junction) and
// at every node the extract does not have, becomes edges from junction to junction, each with its
// class, flags, length and geometry. Edges live in the fine grid's 0.1° cells (graph/<x>_<y>.lgg),
// in the cell of each endpoint (a cross-cell edge is in both; a reader dedupes), so a router loads
// the handful of cells around two points and has every road between them. The phone never fetches
// these; they are the box's own (internal/roadgraph, the day route).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	graphMagic = 0x31474C47 // "GLG1"
	graphHdr   = 12
	// GraphLevel is the cellBuffers key the edges are spooled under during a build.
	GraphLevel = 2
)

// Edge is one stretch of road between two junctions.
type Edge struct {
	From, To int64   // OSM node ids (junctions or way ends)
	Class    uint8   // road class, format.go
	Flags    uint8   // FlagOneway / FlagTunnel / FlagBridge (FlagNamed unused here)
	Meters   uint32  // length along the geometry
	Lat, Lon []int32 // the geometry from From to To, in 1e-7 degrees, endpoints included
}

// GraphName is the file a fine cell's edges are stored in.
func GraphName(c Cell) string { return fmt.Sprintf("graph/%04d_%04d.lgg", c.X, c.Y) }

// encodeEdge is an edge's bytes as they sit in a graph body: from i64, to i64, class u8, flags u8,
// meters u32, n u16, n×(lat i32, lon i32). Little-endian.
func encodeEdge(e Edge) []byte {
	le := binary.LittleEndian
	n := len(e.Lat)
	if n > 65535 {
		n = 65535
	}
	out := make([]byte, 0, 24+8*n)
	out = le.AppendUint64(out, uint64(e.From))
	out = le.AppendUint64(out, uint64(e.To))
	out = append(out, e.Class, e.Flags)
	out = le.AppendUint32(out, e.Meters)
	out = le.AppendUint16(out, uint16(n))
	for i := 0; i < n; i++ {
		out = le.AppendUint32(out, uint32(e.Lat[i]))
		out = le.AppendUint32(out, uint32(e.Lon[i]))
	}
	return out
}

// countEdges walks a graph body and returns its edge count (0 if malformed).
func countEdges(body []byte) int {
	le := binary.LittleEndian
	off, count := 0, 0
	for off < len(body) {
		if off+24 > len(body) {
			return 0
		}
		n := int(le.Uint16(body[off+22:]))
		off += 24 + 8*n
		if off > len(body) {
			return 0
		}
		count++
	}
	return count
}

// graphHeader is a cell file's first 12 bytes.
func graphHeader(c Cell, count int) []byte {
	le := binary.LittleEndian
	var h [graphHdr]byte
	le.PutUint32(h[0:], graphMagic)
	le.PutUint16(h[4:], uint16(c.X))
	le.PutUint16(h[6:], uint16(c.Y))
	le.PutUint32(h[8:], uint32(count))
	return h[:]
}

// DecodeGraph reads a cell file back: the cell and its edges.
func DecodeGraph(p []byte) (Cell, []Edge, error) {
	le := binary.LittleEndian
	if len(p) < graphHdr || le.Uint32(p) != graphMagic {
		return Cell{}, nil, errors.New("not a road graph cell")
	}
	c := Cell{0, int(le.Uint16(p[4:])), int(le.Uint16(p[6:]))}
	count := int(le.Uint32(p[8:]))
	off := graphHdr
	edges := make([]Edge, 0, count)
	for i := 0; i < count; i++ {
		if off+24 > len(p) {
			return c, edges, errors.New("short edge")
		}
		e := Edge{From: int64(le.Uint64(p[off:])), To: int64(le.Uint64(p[off+8:])), Class: p[off+16], Flags: p[off+17], Meters: le.Uint32(p[off+18:])}
		n := int(le.Uint16(p[off+22:]))
		off += 24
		if off+8*n > len(p) {
			return c, edges, errors.New("short geometry")
		}
		e.Lat = make([]int32, n)
		e.Lon = make([]int32, n)
		for k := 0; k < n; k++ {
			e.Lat[k] = int32(le.Uint32(p[off+8*k:]))
			e.Lon[k] = int32(le.Uint32(p[off+8*k+4:]))
		}
		off += 8 * n
		edges = append(edges, e)
	}
	return c, edges, nil
}

// nodePt is a way's node as pass 3 sees it: id and position, or a gap where the extract lacks it.
type nodePt struct {
	id       int64
	lon, lat float64
	ok       bool
}

// emitEdges splits a way at its junctions (nodes the twice bitmap marks) and at missing nodes,
// and spools each edge under the fine cell of both its endpoints.
func emitEdges(pts []nodePt, class, flags uint8, twice *bitmap, cells *cellBuffers) error {
	start := -1
	for i := range pts {
		if !pts[i].ok {
			if start >= 0 && i-1 > start {
				if err := emitEdge(pts[start:i], class, flags, cells); err != nil {
					return err
				}
			}
			start = -1
			continue
		}
		if start < 0 {
			start = i
			continue
		}
		if twice.has(pts[i].id) || i == len(pts)-1 {
			if err := emitEdge(pts[start:i+1], class, flags, cells); err != nil {
				return err
			}
			start = i
		}
	}
	return nil
}

func emitEdge(run []nodePt, class, flags uint8, cells *cellBuffers) error {
	if len(run) < 2 {
		return nil
	}
	e := Edge{From: run[0].id, To: run[len(run)-1].id, Class: class, Flags: flags}
	e.Lat = make([]int32, len(run))
	e.Lon = make([]int32, len(run))
	var m float64
	for i, p := range run {
		e.Lat[i] = int32(math.Round(p.lat * 1e7))
		e.Lon[i] = int32(math.Round(p.lon * 1e7))
		if i > 0 {
			m += Haversine(run[i-1].lat, run[i-1].lon, p.lat, p.lon)
		}
	}
	e.Meters = uint32(math.Round(m))
	b := encodeEdge(e)
	a := CellAt(0, run[0].lon, run[0].lat)
	a.Level = GraphLevel
	if err := cells.add(a, b); err != nil {
		return err
	}
	z := CellAt(0, run[len(run)-1].lon, run[len(run)-1].lat)
	z.Level = GraphLevel
	if z != a {
		return cells.add(z, b)
	}
	return nil
}

// Haversine is the great-circle distance in metres.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := p2-p1, (lon2-lon1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}
