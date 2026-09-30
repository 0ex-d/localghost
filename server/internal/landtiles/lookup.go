package landtiles

// Lookup answers "is this point on land?" from the tiles a Build left on disk, for the box's own use
// (the trail questions ask whether a hop crosses the sea). The index says water or all land for most
// cells; a coast cell's rings are read once and tested with the even-odd rule. A coast cell can hold
// a hundred thousand vertices of mainland, so its edges are filed by horizontal band once, and a
// test looks only at the edges in the band of the point's latitude. Nothing here is sent anywhere:
// the same tiles the phone draws, read in place.

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// Lookup reads tiles under one directory (<mount>/landtiles). Safe for concurrent use.
type Lookup struct {
	dir   string
	mu    sync.Mutex
	tried bool
	idx   []byte // Cols*Rows bytes; nil when the index is missing or unreadable
	tiles map[int]*bandedTile
}

// bandedTile is a coast cell's edges in the cell's quantised units, filed by horizontal band.
type bandedTile struct {
	x1, y1, x2, y2 []float32
	bands          [][]int32
}

const (
	maxTiles   = 16  // coast cells kept in memory; past it the cache starts over
	bandsPer   = 512 // bands per cell: about 200 m of latitude each
	bandHeight = float64(Q) / bandsPer
)

// NewLookup reads nothing until asked.
func NewLookup(dir string) *Lookup { return &Lookup{dir: dir} }

// Reset forgets what was read (after a rebuild swaps the tiles).
func (l *Lookup) Reset() {
	l.mu.Lock()
	l.tried, l.idx, l.tiles = false, nil, nil
	l.mu.Unlock()
}

// OnLand reports whether the point is on land, and whether the box can tell at all (known is false
// when there are no tiles, or a coast cell's tile cannot be read).
func (l *Lookup) OnLand(lat, lon float64) (land, known bool) {
	if l == nil || math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat >= 90 || lon < -180 || lon >= 180 {
		return false, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.tried {
		l.tried = true
		b, err := os.ReadFile(filepath.Join(l.dir, "index.bin"))
		if err == nil && len(b) == 4+Cols*Rows && binary.LittleEndian.Uint32(b) == indexMagic {
			l.idx = b[4:]
		}
	}
	if l.idx == nil {
		return false, false
	}
	c := Cell{int(math.Floor(lon)) + 180, int(math.Floor(lat)) + 90}
	switch l.idx[c.Key()] {
	case Water:
		return false, true
	case Land:
		return true, true
	}
	t, ok := l.tiles[c.Key()]
	if !ok {
		p, err := os.ReadFile(filepath.Join(l.dir, TileName(c)))
		if err != nil {
			return false, false
		}
		dec, err := Decode(p)
		if err != nil {
			return false, false
		}
		t = band(dec)
		if l.tiles == nil || len(l.tiles) >= maxTiles {
			l.tiles = map[int]*bandedTile{}
		}
		l.tiles[c.Key()] = t
	}
	x := (lon - c.Lon0()) * Q
	y := (lat - c.Lat0()) * Q
	b := int(y / bandHeight)
	if b < 0 || b >= bandsPer {
		return false, true
	}
	inside := false
	for _, e := range t.bands[b] {
		yi, yj := float64(t.y1[e]), float64(t.y2[e])
		if (yi > y) != (yj > y) {
			xi, xj := float64(t.x1[e]), float64(t.x2[e])
			if x < (xj-xi)*(y-yi)/(yj-yi)+xi {
				inside = !inside
			}
		}
	}
	return inside, true
}

// band files every edge of every ring under each band its latitude span touches. Even-odd over all
// rings together is the same as ring by ring, so holes and islands need no special case.
func band(t Tile) *bandedTile {
	bt := &bandedTile{bands: make([][]int32, bandsPer)}
	for _, r := range t.Rings {
		n := len(r) / 2
		for i, j := 0, n-1; i < n; j, i = i, i+1 {
			x1, y1 := float32(r[2*j]), float32(r[2*j+1])
			x2, y2 := float32(r[2*i]), float32(r[2*i+1])
			if y1 == y2 {
				continue // horizontal: never crossed by the test's ray
			}
			e := int32(len(bt.x1))
			bt.x1, bt.y1, bt.x2, bt.y2 = append(bt.x1, x1), append(bt.y1, y1), append(bt.x2, x2), append(bt.y2, y2)
			lo := int(math.Min(float64(y1), float64(y2)) / bandHeight)
			hi := int(math.Max(float64(y1), float64(y2)) / bandHeight)
			if hi >= bandsPer {
				hi = bandsPer - 1
			}
			for k := lo; k <= hi; k++ {
				bt.bands[k] = append(bt.bands[k], e)
			}
		}
	}
	return bt
}
