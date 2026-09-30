package landtiles

import (
	"os"
	"path/filepath"
	"testing"
)

// an island in a coast cell (the cell 20..21E, 39..40N), a land cell east of it, sea elsewhere
func writeTiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	coast := Cell{200, 129}
	land := Cell{201, 129}
	idx := make([]byte, Cols*Rows)
	idx[coast.Key()] = Coast
	idx[land.Key()] = Land
	if err := os.WriteFile(filepath.Join(dir, "index.bin"), EncodeIndex(idx), 0o644); err != nil {
		t.Fatal(err)
	}
	q := func(v float64) uint16 { return uint16(v * Q) }
	// a square island 0.1..0.3 of the cell each way, and a lake-sized hole in it (0.18..0.22)
	island := []uint16{q(0.1), q(0.1), q(0.3), q(0.1), q(0.3), q(0.3), q(0.1), q(0.3)}
	hole := []uint16{q(0.18), q(0.18), q(0.22), q(0.18), q(0.22), q(0.22), q(0.18), q(0.22)}
	tile := Tile{Cell: coast, Rings: [][]uint16{island, hole}}
	if err := os.WriteFile(filepath.Join(dir, TileName(coast)), tile.Encode(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLookupOnLand(t *testing.T) {
	l := NewLookup(writeTiles(t))
	for _, c := range []struct {
		lat, lon    float64
		land, known bool
		what        string
	}{
		{39.15, 20.15, true, true, "on the island"},
		{39.20, 20.20, false, true, "in the hole in the island"},
		{39.50, 20.50, false, true, "the sea in the coast cell"},
		{39.50, 21.50, true, true, "an all-land cell"},
		{10.00, 10.00, false, true, "an all-water cell"},
	} {
		land, known := l.OnLand(c.lat, c.lon)
		if land != c.land || known != c.known {
			t.Fatalf("%s: land=%v known=%v", c.what, land, known)
		}
	}
	// without tiles the box cannot tell
	if _, known := NewLookup(t.TempDir()).OnLand(39.15, 20.15); known {
		t.Fatal("known without an index")
	}
	var nilLookup *Lookup
	if _, known := nilLookup.OnLand(39.15, 20.15); known {
		t.Fatal("a nil lookup knows")
	}
}
