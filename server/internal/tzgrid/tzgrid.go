// Package tzgrid answers "which time zone is this point in" from the timezone-boundary-builder
// polygons (the mirror's tz set: combined-with-oceans.json, every zone including the oceans'),
// rasterised once onto a twentieth-of-a-degree grid (about 5 km) and kept as one file the daemons
// read two bytes of per lookup. A zone border five kilometres off matters to nobody's day: zones
// differ by whole hours and the border is a national one. The box's days used to be UTC days;
// with the trail's newest point in a zone, "today" and "19:00" can mean the person's.
package tzgrid

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	Deg   = 0.05
	Cols  = 7200
	Rows  = 3600
	magic = "LGTZ"
	none  = 0xFFFF
)

// Build rasterises the GeoJSON at geojson into the grid file at out (written whole, then renamed).
func Build(geojson, out string, progress func(string)) (zones int, err error) {
	f, err := os.Open(geojson)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	names, cells, err := rasterise(bufio.NewReaderSize(f, 1<<20), progress)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return 0, err
	}
	tmp := out + ".part"
	w, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	bw := bufio.NewWriterSize(w, 1<<20)
	_, _ = bw.WriteString(magic)
	var hdr [2]byte
	binary.LittleEndian.PutUint16(hdr[:], uint16(len(names)))
	_, _ = bw.Write(hdr[:])
	for _, n := range names {
		_ = bw.WriteByte(byte(len(n)))
		_, _ = bw.WriteString(n)
	}
	buf := make([]byte, 2*Cols)
	for y := 0; y < Rows; y++ {
		for x := 0; x < Cols; x++ {
			binary.LittleEndian.PutUint16(buf[2*x:], cells[y*Cols+x])
		}
		if _, err := bw.Write(buf); err != nil {
			w.Close()
			os.Remove(tmp)
			return 0, err
		}
	}
	if err := bw.Flush(); err != nil {
		w.Close()
		os.Remove(tmp)
		return 0, err
	}
	if err := w.Close(); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return len(names), os.Rename(tmp, out)
}

type feature struct {
	Properties struct {
		TZID string `json:"tzid"`
	} `json:"properties"`
	Geometry struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
}

type polygon [][][2]float64

// rasterise reads features one at a time and fills the grid by cell centre, even-odd.
func rasterise(r io.Reader, progress func(string)) ([]string, []uint16, error) {
	dec := json.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("not a FeatureCollection: %w", err)
		}
		if s, ok := tok.(string); ok && s == "features" {
			if tok, err = dec.Token(); err != nil {
				return nil, nil, err
			}
			if d, ok := tok.(json.Delim); !ok || d != '[' {
				return nil, nil, errors.New("features is not an array")
			}
			break
		}
	}
	cells := make([]uint16, Cols*Rows)
	for i := range cells {
		cells[i] = none
	}
	var names []string
	index := map[string]uint16{}
	n := 0
	for dec.More() {
		var ft feature
		if err := dec.Decode(&ft); err != nil {
			return nil, nil, fmt.Errorf("feature %d: %w", n, err)
		}
		n++
		if ft.Properties.TZID == "" {
			continue
		}
		id, ok := index[ft.Properties.TZID]
		if !ok {
			if len(names) >= none-1 {
				return nil, nil, errors.New("more zones than the grid can name")
			}
			id = uint16(len(names))
			index[ft.Properties.TZID] = id
			names = append(names, ft.Properties.TZID)
		}
		var polys []polygon
		switch ft.Geometry.Type {
		case "Polygon":
			var p polygon
			if err := json.Unmarshal(ft.Geometry.Coordinates, &p); err != nil {
				return nil, nil, err
			}
			polys = []polygon{p}
		case "MultiPolygon":
			if err := json.Unmarshal(ft.Geometry.Coordinates, &polys); err != nil {
				return nil, nil, err
			}
		}
		for _, p := range polys {
			fill(p, func(x, y int) { cells[y*Cols+x] = id })
		}
		if progress != nil && n%50 == 0 {
			progress(fmt.Sprintf("%d zones rasterised", n))
		}
	}
	if len(names) == 0 {
		return nil, nil, errors.New("no zone in the file (is it timezone-boundary-builder's combined-with-oceans.json?)")
	}
	return names, cells, nil
}

// fill marks every cell whose centre is inside the polygon (its outer ring less its holes).
func fill(p polygon, set func(x, y int)) {
	crossings := map[int][]float64{}
	for _, ring := range p {
		n := len(ring)
		if n < 3 {
			continue
		}
		for i := 0; i < n; i++ {
			a, b := ring[i], ring[(i+1)%n]
			lo, hi := a, b
			if lo[1] > hi[1] {
				lo, hi = hi, lo
			}
			if lo[1] == hi[1] {
				continue
			}
			y0 := int(math.Ceil((lo[1]+90)/Deg - 0.5))
			y1 := int(math.Ceil((hi[1]+90)/Deg-0.5)) - 1
			for y := y0; y <= y1; y++ {
				latc := (float64(y)+0.5)*Deg - 90
				crossings[y] = append(crossings[y], lo[0]+(latc-lo[1])*(hi[0]-lo[0])/(hi[1]-lo[1]))
			}
		}
	}
	for y, xs := range crossings {
		if y < 0 || y >= Rows {
			continue
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			x0 := int(math.Ceil((xs[i]+180)/Deg - 0.5))
			x1 := int(math.Ceil((xs[i+1]+180)/Deg-0.5)) - 1
			for x := x0; x <= x1; x++ {
				if x >= 0 && x < Cols {
					set(x, y)
				}
			}
		}
	}
}

// Lookup reads a grid file: the names in memory, the cells on disk (two bytes a lookup).
type Lookup struct {
	f     *os.File
	names []string
	base  int64
}

// Open reads the grid's header. The file stays open.
func Open(path string) (*Lookup, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReader(f)
	head := make([]byte, 6)
	if _, err := io.ReadFull(br, head); err != nil || string(head[:4]) != magic {
		f.Close()
		return nil, errors.New("not a time zone grid")
	}
	n := int(binary.LittleEndian.Uint16(head[4:]))
	l := &Lookup{f: f, base: 6}
	for i := 0; i < n; i++ {
		ln, err := br.ReadByte()
		if err != nil {
			f.Close()
			return nil, err
		}
		name := make([]byte, int(ln))
		if _, err := io.ReadFull(br, name); err != nil {
			f.Close()
			return nil, err
		}
		l.names = append(l.names, string(name))
		l.base += 1 + int64(ln)
	}
	return l, nil
}

// Close closes the file.
func (l *Lookup) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}

// Zone is the IANA name at a point, or "" when the grid has none there.
func (l *Lookup) Zone(lat, lon float64) string {
	if l == nil || math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return ""
	}
	x := int(math.Floor((lon + 180) / Deg))
	y := int(math.Floor((lat + 90) / Deg))
	if x >= Cols {
		x = Cols - 1
	}
	if y >= Rows {
		y = Rows - 1
	}
	var b [2]byte
	if _, err := l.f.ReadAt(b[:], l.base+2*int64(y*Cols+x)); err != nil {
		return ""
	}
	id := binary.LittleEndian.Uint16(b[:])
	if int(id) >= len(l.names) {
		return ""
	}
	return l.names[id]
}

// Zones is every name in the grid.
func (l *Lookup) Zones() []string { return append([]string(nil), l.names...) }

// FindGeoJSON is the zone file under dir: combined-with-oceans.json by name, else any .json or
// .geojson there (the mirror's zip unpacks to one file).
func FindGeoJSON(dir string) string {
	for _, n := range []string{"combined-with-oceans.json", "timezones-with-oceans.geojson", "combined.json", "timezones.geojson"} {
		if fi, err := os.Stat(filepath.Join(dir, n)); err == nil && !fi.IsDir() {
			return filepath.Join(dir, n)
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".geojson")) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// Stale says whether the grid wants building: no grid, or the GeoJSON is newer.
func Stale(geojson, grid string) bool {
	g, err := os.Stat(grid)
	if err != nil {
		return true
	}
	j, err := os.Stat(geojson)
	if err != nil {
		return false
	}
	return j.ModTime().After(g.ModTime())
}

// Location is the zone's rules, from the Go toolchain's own copy when the OS has none
// (time/tzdata is linked into the daemons that call this); nil for a name nothing knows.
func Location(name string) *time.Location {
	if name == "" {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return loc
}
