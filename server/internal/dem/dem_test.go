package dem

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// tiffSpec is a test GeoTIFF: w x h heights (100*row + col), cut into chunks of cw x ch (a strip
// when cw == w), written the way asked.
type tiffSpec struct {
	w, h, cw, ch int
	bo           binary.ByteOrder
	big          bool
	format, bits int    // 3/32 float, 2/16 int
	comp, pred   uint16 // 1, 5, 8 ; 1, 2, 3
	point        bool   // pixel is point
	lat, lon     int    // the south-west corner of a one-degree tile
	nodata       string
}

func (s tiffSpec) value(row, col int) float64 { return float64(100*row + col) }

func (s tiffSpec) chunk(tx, ty int) []byte {
	bps := s.bits / 8
	rows := s.ch
	if s.cw == s.w && (ty+1)*s.ch > s.h {
		rows = s.h - ty*s.ch
	}
	buf := make([]byte, s.cw*rows*bps)
	for r := 0; r < rows; r++ {
		row := buf[r*s.cw*bps : (r+1)*s.cw*bps]
		for c := 0; c < s.cw; c++ {
			y, x := ty*s.ch+r, tx*s.cw+c
			v := 0.0
			if y < s.h && x < s.w {
				v = s.value(y, x)
			}
			if s.format == 3 {
				binary.BigEndian.PutUint32(row[c*4:], math.Float32bits(float32(v))) // big-endian planes for the float predictor
				if s.pred != 3 {
					s.bo.PutUint32(row[c*4:], math.Float32bits(float32(v)))
				}
			} else {
				s.bo.PutUint16(row[c*2:], uint16(int16(v)))
			}
		}
		switch s.pred {
		case 2: // integer differences, right to left
			for c := s.cw - 1; c > 0; c-- {
				s.bo.PutUint16(row[c*2:], s.bo.Uint16(row[c*2:])-s.bo.Uint16(row[(c-1)*2:]))
			}
		case 3: // byte planes most significant first, then byte differences
			planes := make([]byte, len(row))
			for i := 0; i < s.cw; i++ {
				for k := 0; k < bps; k++ {
					planes[k*s.cw+i] = row[i*bps+k]
				}
			}
			for i := len(planes) - 1; i > 0; i-- {
				planes[i] -= planes[i-1]
			}
			copy(row, planes)
		}
	}
	switch s.comp {
	case 8:
		var b bytes.Buffer
		zw := zlib.NewWriter(&b)
		zw.Write(buf)
		zw.Close()
		return b.Bytes()
	case 5:
		return lzwEncode(buf)
	}
	return buf
}

// lzwEncode is TIFF's LZW the way libtiff writes it.
func lzwEncode(src []byte) []byte {
	var out bytes.Buffer
	var acc uint64
	n := 0
	width := 9
	put := func(code int) {
		acc = acc<<uint(width) | uint64(code)
		n += width
		for n >= 8 {
			out.WriteByte(byte(acc >> uint(n-8)))
			n -= 8
		}
	}
	dict := map[string]int{}
	next := 258
	put(256)
	grow := func() {
		next++
		if next == 4094 { // the table is full: libtiff clears it
			put(256)
			dict = map[string]int{}
			next, width = 258, 9
			return
		}
		if next > 1<<width-1 && width < 12 {
			width++
		}
	}
	w := string(src[:1])
	for _, c := range src[1:] {
		wc := w + string([]byte{c})
		if len(wc) == 1 {
			w = wc
			continue
		}
		if _, ok := dict[wc]; ok {
			w = wc
			continue
		}
		code := int(w[0])
		if len(w) > 1 {
			code = dict[w]
		}
		put(code)
		dict[wc] = next
		grow()
		w = string([]byte{c})
	}
	code := int(w[0])
	if len(w) > 1 {
		code = dict[w]
	}
	put(code)
	grow()
	put(257)
	if n > 0 {
		out.WriteByte(byte(acc << uint(8-n)))
	}
	return out.Bytes()
}

func (s tiffSpec) build() []byte {
	type ent struct {
		tag, typ uint16
		vals     any // []uint64 (typ 3/4/16), []float64 (12), string (2)
	}
	across, down := (s.w+s.cw-1)/s.cw, (s.h+s.ch-1)/s.ch
	var chunks [][]byte
	for ty := 0; ty < down; ty++ {
		for tx := 0; tx < across; tx++ {
			chunks = append(chunks, s.chunk(tx, ty))
		}
	}
	sx := 1.0 / float64(s.w)
	sy := 1.0 / float64(s.h)
	tie := []float64{0, 0, 0, float64(s.lon), float64(s.lat + 1), 0}
	rt := uint64(1)
	if s.point {
		rt = 2
		tie[3] += sx / 2
		tie[4] -= sy / 2
	}
	var data bytes.Buffer
	hdr := 8
	if s.big {
		hdr = 16
	}
	var offs, counts []uint64
	for _, c := range chunks {
		offs = append(offs, uint64(hdr+data.Len()))
		counts = append(counts, uint64(len(c)))
		data.Write(c)
	}
	ents := []ent{
		{256, 3, []uint64{uint64(s.w)}}, {257, 3, []uint64{uint64(s.h)}}, {258, 3, []uint64{uint64(s.bits)}},
		{259, 3, []uint64{uint64(s.comp)}}, {277, 3, []uint64{1}}, {317, 3, []uint64{uint64(s.pred)}},
		{339, 3, []uint64{uint64(s.format)}},
		{33550, 12, []float64{sx, sy, 0}}, {33922, 12, tie},
		{34735, 3, []uint64{1, 1, 0, 2, 1024, 0, 1, 2, 1025, 0, 1, rt}},
	}
	if s.cw == s.w {
		ents = append(ents, ent{273, 16, offs}, ent{278, 3, []uint64{uint64(s.ch)}}, ent{279, 16, counts})
	} else {
		ents = append(ents, ent{322, 3, []uint64{uint64(s.cw)}}, ent{323, 3, []uint64{uint64(s.ch)}}, ent{324, 16, offs}, ent{325, 16, counts})
	}
	if s.nodata != "" {
		ents = append(ents, ent{42113, 2, s.nodata})
	}
	if !s.big {
		for i := range ents {
			if ents[i].typ == 16 {
				ents[i].typ = 4
			}
		}
	}
	// sorted by tag, as TIFF wants
	for i := range ents {
		for j := i + 1; j < len(ents); j++ {
			if ents[j].tag < ents[i].tag {
				ents[i], ents[j] = ents[j], ents[i]
			}
		}
	}
	ifdAt := uint64(hdr + data.Len())
	ew, cw := 12, 2
	if s.big {
		ew, cw = 20, 8
	}
	extraAt := ifdAt + uint64(cw+len(ents)*ew+8)
	var ifd, extra bytes.Buffer
	if s.big {
		binary.Write(&ifd, s.bo, uint64(len(ents)))
	} else {
		binary.Write(&ifd, s.bo, uint16(len(ents)))
	}
	for _, e := range ents {
		var val bytes.Buffer
		count := 0
		switch v := e.vals.(type) {
		case []uint64:
			count = len(v)
			for _, x := range v {
				switch e.typ {
				case 3:
					binary.Write(&val, s.bo, uint16(x))
				case 4:
					binary.Write(&val, s.bo, uint32(x))
				default:
					binary.Write(&val, s.bo, x)
				}
			}
		case []float64:
			count = len(v)
			for _, x := range v {
				binary.Write(&val, s.bo, x)
			}
		case string:
			count = len(v) + 1
			val.WriteString(v)
			val.WriteByte(0)
		}
		binary.Write(&ifd, s.bo, e.tag)
		binary.Write(&ifd, s.bo, e.typ)
		slot := 4
		if s.big {
			binary.Write(&ifd, s.bo, uint64(count))
			slot = 8
		} else {
			binary.Write(&ifd, s.bo, uint32(count))
		}
		if val.Len() <= slot {
			b := make([]byte, slot)
			copy(b, val.Bytes())
			ifd.Write(b)
		} else {
			at := extraAt + uint64(extra.Len())
			if s.big {
				binary.Write(&ifd, s.bo, at)
			} else {
				binary.Write(&ifd, s.bo, uint32(at))
			}
			extra.Write(val.Bytes())
			if extra.Len()%2 == 1 {
				extra.WriteByte(0)
			}
		}
	}
	if s.big {
		binary.Write(&ifd, s.bo, uint64(0))
	} else {
		binary.Write(&ifd, s.bo, uint32(0))
		ifd.Write(make([]byte, 4))
	}
	var out bytes.Buffer
	if s.bo == binary.LittleEndian {
		out.WriteString("II")
	} else {
		out.WriteString("MM")
	}
	if s.big {
		binary.Write(&out, s.bo, uint16(43))
		binary.Write(&out, s.bo, uint16(8))
		binary.Write(&out, s.bo, uint16(0))
		binary.Write(&out, s.bo, ifdAt)
	} else {
		binary.Write(&out, s.bo, uint16(42))
		binary.Write(&out, s.bo, uint32(ifdAt))
	}
	out.Write(data.Bytes())
	out.Write(ifd.Bytes())
	out.Write(extra.Bytes())
	return out.Bytes()
}

func TestReadsEveryKindOfTile(t *testing.T) {
	for name, s := range map[string]tiffSpec{
		"float deflate predictor 3 tiled (Copernicus)": {w: 12, h: 12, cw: 8, ch: 8, bo: binary.LittleEndian, format: 3, bits: 32, comp: 8, pred: 3, lat: 51, lon: -1},
		"float stored strips big-endian":               {w: 12, h: 12, cw: 12, ch: 5, bo: binary.BigEndian, format: 3, bits: 32, comp: 1, pred: 1, lat: 51, lon: -1},
		"int16 LZW predictor 2 strips":                 {w: 12, h: 12, cw: 12, ch: 4, bo: binary.LittleEndian, format: 2, bits: 16, comp: 5, pred: 2, lat: 51, lon: -1},
		"float big TIFF pixel is point":                {w: 12, h: 12, cw: 4, ch: 4, bo: binary.LittleEndian, big: true, format: 3, bits: 32, comp: 8, pred: 1, point: true, lat: 51, lon: -1},
		"large LZW strip":                              {w: 300, h: 40, cw: 300, ch: 40, bo: binary.LittleEndian, format: 2, bits: 16, comp: 5, pred: 1, lat: 51, lon: -1},
	} {
		b := s.build()
		tl, err := ReadTile(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tl.W != s.w || tl.H != s.h {
			t.Fatalf("%s: %dx%d", name, tl.W, tl.H)
		}
		for _, rc := range [][2]int{{0, 0}, {3, 7}, {s.h - 1, s.w - 1}, {5, 0}} {
			r, c := rc[0], rc[1]
			if got := tl.Data[r*s.w+c]; float64(got) != s.value(r, c) {
				t.Fatalf("%s: (%d,%d) %v want %v", name, r, c, got, s.value(r, c))
			}
			// the pixel's centre
			lat := float64(s.lat+1) - (float64(r)+0.5)/float64(s.h)
			lon := float64(s.lon) + (float64(c)+0.5)/float64(s.w)
			if h, ok := tl.At(lat, lon); !ok || math.Abs(h-s.value(r, c)) > 1e-6 {
				t.Fatalf("%s: at (%d,%d) %v %v", name, r, c, h, ok)
			}
		}
		// halfway between two centres along a row: the mean
		lat := float64(s.lat+1) - 2.5/float64(s.h)
		lon := float64(s.lon) + 4.0/float64(s.w)
		if h, ok := tl.At(lat, lon); !ok || math.Abs(h-(s.value(2, 3)+s.value(2, 4))/2) > 1e-6 {
			t.Fatalf("%s: between %v", name, h)
		}
		if _, ok := tl.At(float64(s.lat)+2, float64(s.lon)); ok {
			t.Fatalf("%s: outside the tile", name)
		}
	}
}

func TestNoDataAndRefusals(t *testing.T) {
	s := tiffSpec{w: 4, h: 4, cw: 4, ch: 4, bo: binary.LittleEndian, format: 3, bits: 32, comp: 1, pred: 1, lat: 0, lon: 0, nodata: "0"}
	b := s.build()
	tl, err := ReadTile(bytes.NewReader(b), int64(len(b)))
	if err != nil || !tl.HasNoData || tl.NoData != 0 {
		t.Fatal(err, tl.HasNoData)
	}
	// the corner pixel is 0 (no data): its centre has no height
	if _, ok := tl.At(1-0.5/4, 0.5/4); ok {
		t.Fatal("no data is not a height")
	}
	if _, err := ReadTile(bytes.NewReader([]byte("not a tiff at all")), 17); err == nil {
		t.Fatal("not a TIFF")
	}
}

func TestSetFindsTilesByName(t *testing.T) {
	dir := t.TempDir()
	s := tiffSpec{w: 12, h: 12, cw: 8, ch: 8, bo: binary.LittleEndian, format: 3, bits: 32, comp: 8, pred: 3, lat: 51, lon: -1}
	if err := os.WriteFile(filepath.Join(dir, "Copernicus_DSM_COG_30_N51_00_W001_00_DEM.tif"), s.build(), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "tileList.txt"), []byte("x"), 0o644)
	set := Open(dir)
	if set.Tiles() != 1 {
		t.Fatal(set.Tiles())
	}
	h, ok := set.Height(52-0.5/12, -1+0.5/12)
	if !ok || h != 0 {
		t.Fatal(h, ok)
	}
	if _, ok := set.Height(40, 10); ok {
		t.Fatal("no tile there")
	}
	for name, want := range map[string][2]int{
		"Copernicus_DSM_COG_30_S12_00_E130_00_DEM.tif": {-12, 130},
		"Copernicus_DSM_COG_30_N00_00_W180_00_DEM":     {0, -180},
	} {
		if a, b, ok := Corner(name); !ok || a != want[0] || b != want[1] {
			t.Fatal(name, a, b, ok)
		}
	}
	if _, _, ok := Corner("NOTICE.txt"); ok {
		t.Fatal("not a tile")
	}
	var nilSet *Set
	if _, ok := nilSet.Height(1, 1); ok || nilSet.Tiles() != 0 {
		t.Fatal("no set")
	}
}

func TestClimb(t *testing.T) {
	// up 100 with jitter, down 60, up 30: jitter under the threshold does not count
	hs := []float64{10, 12, 9, 50, 48, 110, 105, 108, 50, 52, 80}
	up, down, high, low, ok := Climb(hs, 5)
	if !ok || up != 100+30 || down != 60 || high != 110 || low != 9 {
		t.Fatal(up, down, high, low)
	}
	if _, _, _, _, ok := Climb([]float64{1}, 5); ok {
		t.Fatal("one height")
	}
}
