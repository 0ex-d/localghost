package dem

// A GeoTIFF reader for elevation tiles, the standard library and nothing else: classic and big
// TIFF, either byte order, tiled or in strips, stored as they are, Deflate (zlib) or LZW, the
// horizontal and the floating-point predictors, samples of float32, float64, int16, uint16 or
// int32; the georeferencing from the pixel scale, the tie point and the raster type (pixel is
// area or point), and GDAL's no-data value. Only the first image (a cloud-optimised file's full
// resolution; its overviews follow it and are not read).

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Tile is one elevation tile in memory: heights in metres, row by row from the north.
type Tile struct {
	W, H      int
	Data      []float32
	NoData    float64
	HasNoData bool
	// pixel centre (col, row) is at lon = X0 + col*SX, lat = Y0 - row*SY
	X0, Y0, SX, SY float64
}

const (
	tImageWidth   = 256
	tImageLength  = 257
	tBitsPerSamp  = 258
	tCompression  = 259
	tStripOffsets = 273
	tSamplesPP    = 277
	tRowsPerStrip = 278
	tStripCounts  = 279
	tPlanar       = 284
	tPredictor    = 317
	tTileWidth    = 322
	tTileLength   = 323
	tTileOffsets  = 324
	tTileCounts   = 325
	tSampleFormat = 339
	tPixelScale   = 33550
	tTiepoint     = 33922
	tGeoKeys      = 34735
	tGDALNoData   = 42113

	maxPixels = 16 << 20 // a tile larger than this is not an elevation tile the box reads
)

type ifdEntry struct {
	typ   uint16
	count uint64
	raw   []byte // the value bytes, read
}

type tiff struct {
	r       io.ReaderAt
	size    int64
	bo      binary.ByteOrder
	big     bool
	entries map[uint16]ifdEntry
}

var typeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8, 16: 8, 17: 8, 18: 8}

// ReadTile reads a GeoTIFF elevation tile.
func ReadTile(r io.ReaderAt, size int64) (*Tile, error) {
	t := &tiff{r: r, size: size, entries: map[uint16]ifdEntry{}}
	var h [16]byte
	if _, err := r.ReadAt(h[:8], 0); err != nil {
		return nil, fmt.Errorf("tiff: header: %w", err)
	}
	switch string(h[:2]) {
	case "II":
		t.bo = binary.LittleEndian
	case "MM":
		t.bo = binary.BigEndian
	default:
		return nil, errors.New("tiff: not a TIFF file")
	}
	var ifd uint64
	switch t.bo.Uint16(h[2:]) {
	case 42:
		ifd = uint64(t.bo.Uint32(h[4:]))
	case 43:
		if _, err := r.ReadAt(h[:16], 0); err != nil {
			return nil, err
		}
		if t.bo.Uint16(h[4:]) != 8 {
			return nil, errors.New("tiff: a big TIFF with offsets that are not 8 bytes")
		}
		t.big = true
		ifd = t.bo.Uint64(h[8:])
	default:
		return nil, errors.New("tiff: not a TIFF file (version)")
	}
	if err := t.readIFD(ifd); err != nil {
		return nil, err
	}
	return t.tile()
}

func (t *tiff) readIFD(off uint64) error {
	cw, ew := 2, 12
	if t.big {
		cw, ew = 8, 20
	}
	if off == 0 || off+uint64(cw) > uint64(t.size) {
		return errors.New("tiff: no image directory")
	}
	cb := make([]byte, cw)
	if _, err := t.r.ReadAt(cb, int64(off)); err != nil {
		return err
	}
	n := uint64(t.bo.Uint16(cb))
	if t.big {
		n = t.bo.Uint64(cb)
	}
	if n > 4096 || off+uint64(cw)+n*uint64(ew) > uint64(t.size) {
		return errors.New("tiff: a damaged image directory")
	}
	b := make([]byte, n*uint64(ew))
	if _, err := t.r.ReadAt(b, int64(off)+int64(cw)); err != nil {
		return err
	}
	for i := uint64(0); i < n; i++ {
		e := b[i*uint64(ew):]
		tag, typ := t.bo.Uint16(e), t.bo.Uint16(e[2:])
		var count uint64
		var val []byte
		if t.big {
			count, val = t.bo.Uint64(e[4:]), e[12:20]
		} else {
			count, val = uint64(t.bo.Uint32(e[4:])), e[8:12]
		}
		sz, ok := typeSize[typ]
		if !ok {
			continue
		}
		total := count * uint64(sz)
		if total > 64<<20 {
			return fmt.Errorf("tiff: tag %d is too large", tag)
		}
		var raw []byte
		if total <= uint64(len(val)) {
			raw = append([]byte(nil), val[:total]...)
		} else {
			at := uint64(t.bo.Uint32(val))
			if t.big {
				at = t.bo.Uint64(val)
			}
			if at+total > uint64(t.size) {
				return fmt.Errorf("tiff: tag %d points outside the file", tag)
			}
			raw = make([]byte, total)
			if _, err := t.r.ReadAt(raw, int64(at)); err != nil {
				return err
			}
		}
		t.entries[tag] = ifdEntry{typ: typ, count: count, raw: raw}
	}
	return nil
}

// ints is a tag's values as integers (BYTE, SHORT, LONG, LONG8 and their signed forms).
func (t *tiff) ints(tag uint16) []uint64 {
	e, ok := t.entries[tag]
	if !ok {
		return nil
	}
	sz := typeSize[e.typ]
	out := make([]uint64, 0, e.count)
	for i := uint64(0); i < e.count; i++ {
		p := e.raw[i*uint64(sz):]
		switch e.typ {
		case 1, 6, 7:
			out = append(out, uint64(p[0]))
		case 3, 8:
			out = append(out, uint64(t.bo.Uint16(p)))
		case 4, 9:
			out = append(out, uint64(t.bo.Uint32(p)))
		case 16, 17, 18:
			out = append(out, t.bo.Uint64(p))
		default:
			return nil
		}
	}
	return out
}

func (t *tiff) int1(tag uint16, def uint64) uint64 {
	if v := t.ints(tag); len(v) > 0 {
		return v[0]
	}
	return def
}

func (t *tiff) doubles(tag uint16) []float64 {
	e, ok := t.entries[tag]
	if !ok {
		return nil
	}
	var out []float64
	for i := uint64(0); i < e.count; i++ {
		switch e.typ {
		case 12:
			out = append(out, math.Float64frombits(t.bo.Uint64(e.raw[i*8:])))
		case 11:
			out = append(out, float64(math.Float32frombits(t.bo.Uint32(e.raw[i*4:]))))
		}
	}
	return out
}

func (t *tiff) ascii(tag uint16) string {
	e, ok := t.entries[tag]
	if !ok || e.typ != 2 {
		return ""
	}
	return strings.TrimRight(string(e.raw), "\x00 ")
}

func (t *tiff) tile() (*Tile, error) {
	w, h := int(t.int1(tImageWidth, 0)), int(t.int1(tImageLength, 0))
	if w <= 0 || h <= 0 || w*h > maxPixels {
		return nil, fmt.Errorf("tiff: an image of %d x %d", w, h)
	}
	if spp := t.int1(tSamplesPP, 1); spp != 1 {
		return nil, fmt.Errorf("tiff: %d samples a pixel (an elevation tile has one)", spp)
	}
	bits := int(t.int1(tBitsPerSamp, 1))
	format := t.int1(tSampleFormat, 1)
	bps := bits / 8
	switch {
	case format == 3 && (bits == 32 || bits == 64):
	case (format == 1 || format == 2) && (bits == 16 || bits == 32):
	default:
		return nil, fmt.Errorf("tiff: samples of %d bits, format %d", bits, format)
	}
	comp, pred := t.int1(tCompression, 1), t.int1(tPredictor, 1)
	out := &Tile{W: w, H: h, Data: make([]float32, w*h)}
	// the chunks: tiles, or strips (a strip is a tile as wide as the image)
	tw, th := int(t.int1(tTileWidth, 0)), int(t.int1(tTileLength, 0))
	offs, counts := t.ints(tTileOffsets), t.ints(tTileCounts)
	if tw == 0 || th == 0 {
		tw, th = w, int(t.int1(tRowsPerStrip, uint64(h)))
		if th <= 0 || th > h {
			th = h
		}
		offs, counts = t.ints(tStripOffsets), t.ints(tStripCounts)
	}
	across, down := (w+tw-1)/tw, (h+th-1)/th
	if len(offs) < across*down || len(counts) < across*down {
		return nil, errors.New("tiff: fewer chunks than the image needs")
	}
	for ty := 0; ty < down; ty++ {
		for tx := 0; tx < across; tx++ {
			i := ty*across + tx
			rows := th
			if tw == w && (ty+1)*th > h { // the last strip may be short
				rows = h - ty*th
			}
			need := tw * rows * bps
			raw := make([]byte, counts[i])
			if offs[i]+counts[i] > uint64(t.size) {
				return nil, errors.New("tiff: a chunk outside the file")
			}
			if _, err := t.r.ReadAt(raw, int64(offs[i])); err != nil && err != io.EOF {
				return nil, err
			}
			buf, err := decompress(raw, comp, need)
			if err != nil {
				return nil, err
			}
			if len(buf) < need {
				return nil, fmt.Errorf("tiff: chunk %d holds %d bytes, %d needed", i, len(buf), need)
			}
			buf = buf[:need]
			vals, err := t.samples(buf, tw, rows, bps, format, pred)
			if err != nil {
				return nil, err
			}
			for r := 0; r < rows; r++ {
				y := ty*th + r
				if y >= h {
					break
				}
				for c := 0; c < tw; c++ {
					x := tx*tw + c
					if x >= w {
						break
					}
					out.Data[y*w+x] = vals[r*tw+c]
				}
			}
		}
	}
	if s := t.ascii(tGDALNoData); s != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			out.NoData, out.HasNoData = v, true
		}
	}
	scale, tie := t.doubles(tPixelScale), t.doubles(tTiepoint)
	if len(scale) < 2 || len(tie) < 6 || scale[0] <= 0 || scale[1] <= 0 {
		return nil, errors.New("tiff: no georeferencing (pixel scale and tie point)")
	}
	out.SX, out.SY = scale[0], scale[1]
	// the tie point names a raster point (I, J) and where it is (X, Y); pixel is area (the
	// default) puts (0, 0) at a pixel's corner, pixel is point at its centre
	half := 0.5
	if rasterType(t.ints(tGeoKeys)) == 2 {
		half = 0
	}
	out.X0 = tie[3] + (half-tie[0])*out.SX
	out.Y0 = tie[4] - (half-tie[1])*out.SY
	return out, nil
}

// rasterType is GTRasterTypeGeoKey (1025): 1 pixel is area, 2 pixel is point; 1 when not said.
func rasterType(keys []uint64) uint64 {
	if len(keys) < 4 {
		return 1
	}
	n := int(keys[3])
	for i := 0; i < n && 4+4*i+3 < len(keys); i++ {
		k := keys[4+4*i:]
		if k[0] == 1025 && k[1] == 0 && k[2] == 1 {
			return k[3]
		}
	}
	return 1
}

func decompress(raw []byte, comp uint64, need int) ([]byte, error) {
	switch comp {
	case 1:
		return raw, nil
	case 8, 32946:
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("tiff: deflate: %w", err)
		}
		defer zr.Close()
		out := make([]byte, need)
		n, err := io.ReadFull(zr, out)
		if err != nil && err != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("tiff: deflate: %w", err)
		}
		return out[:n], nil
	case 5:
		return lzwDecode(raw, need)
	}
	return nil, fmt.Errorf("tiff: compression %d is not one this reader knows", comp)
}

// samples turns a chunk's bytes into heights, undoing the predictor (2: each sample the
// difference from the one to its left; 3: the floating-point predictor, bytes differenced then
// laid out most significant first).
func (t *tiff) samples(buf []byte, w, rows, bps int, format, pred uint64) ([]float32, error) {
	out := make([]float32, w*rows)
	rowLen := w * bps
	switch pred {
	case 1:
	case 2:
		if format == 3 {
			return nil, errors.New("tiff: the horizontal predictor on floating-point samples")
		}
		for r := 0; r < rows; r++ {
			row := buf[r*rowLen : (r+1)*rowLen]
			for c := 1; c < w; c++ {
				p, q := row[(c-1)*bps:], row[c*bps:]
				switch bps {
				case 2:
					t.bo.PutUint16(q, t.bo.Uint16(q)+t.bo.Uint16(p))
				case 4:
					t.bo.PutUint32(q, t.bo.Uint32(q)+t.bo.Uint32(p))
				}
			}
		}
	case 3:
		if format != 3 {
			return nil, errors.New("tiff: the floating-point predictor on integer samples")
		}
		tmp := make([]byte, rowLen)
		for r := 0; r < rows; r++ {
			row := buf[r*rowLen : (r+1)*rowLen]
			for i := 1; i < rowLen; i++ {
				row[i] += row[i-1]
			}
			for i := 0; i < w; i++ {
				for k := 0; k < bps; k++ {
					tmp[i*bps+k] = row[k*w+i] // big-endian bytes of sample i
				}
			}
			for i := 0; i < w; i++ {
				p := tmp[i*bps:]
				if bps == 4 {
					out[r*w+i] = math.Float32frombits(binary.BigEndian.Uint32(p))
				} else {
					out[r*w+i] = float32(math.Float64frombits(binary.BigEndian.Uint64(p)))
				}
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("tiff: predictor %d", pred)
	}
	for i := range out {
		p := buf[i*bps:]
		switch {
		case format == 3 && bps == 4:
			out[i] = math.Float32frombits(t.bo.Uint32(p))
		case format == 3 && bps == 8:
			out[i] = float32(math.Float64frombits(t.bo.Uint64(p)))
		case format == 2 && bps == 2:
			out[i] = float32(int16(t.bo.Uint16(p)))
		case format == 1 && bps == 2:
			out[i] = float32(t.bo.Uint16(p))
		case format == 2 && bps == 4:
			out[i] = float32(int32(t.bo.Uint32(p)))
		default:
			out[i] = float32(t.bo.Uint32(p))
		}
	}
	return out, nil
}

// lzwDecode is TIFF's LZW: codes most significant bit first, 9 to 12 bits wide, the width growing
// one code early (at 511, 1023 and 2047), 256 to clear and 257 to end.
func lzwDecode(src []byte, need int) ([]byte, error) {
	const clear, eoi = 256, 257
	out := make([]byte, 0, need)
	var table [][]byte
	reset := func() {
		table = table[:0]
		for i := 0; i < 256; i++ {
			table = append(table, []byte{byte(i)})
		}
		table = append(table, nil, nil)
	}
	reset()
	width := 9
	var acc uint32
	nbits := 0
	pos := 0
	next := func() (int, bool) {
		for nbits < width {
			if pos >= len(src) {
				return 0, false
			}
			acc = acc<<8 | uint32(src[pos])
			pos++
			nbits += 8
		}
		code := int(acc>>(uint(nbits-width))) & (1<<width - 1)
		nbits -= width
		return code, true
	}
	var prev []byte
	for len(out) < need {
		code, ok := next()
		if !ok || code == eoi {
			break
		}
		if code == clear {
			reset()
			width = 9
			prev = nil
			continue
		}
		var entry []byte
		switch {
		case code < len(table) && table[code] != nil:
			entry = table[code]
		case code == len(table) && prev != nil:
			entry = append(append([]byte(nil), prev...), prev[0])
		default:
			return nil, errors.New("tiff: a damaged LZW stream")
		}
		out = append(out, entry...)
		if prev != nil && len(table) < 4096 {
			table = append(table, append(append([]byte(nil), prev...), entry[0]))
		}
		prev = entry
		switch len(table) {
		case 511:
			width = 10
		case 1023:
			width = 11
		case 2047:
			width = 12
		}
	}
	return out, nil
}

// At is the height at a place, bilinear between the four pixel centres around it; ok is false
// outside the tile or on no-data.
func (t *Tile) At(lat, lon float64) (float64, bool) {
	fx := (lon - t.X0) / t.SX
	fy := (t.Y0 - lat) / t.SY
	// a place in the outer half pixel uses the edge pixel
	if fx < -0.5 || fy < -0.5 || fx > float64(t.W)-0.5 || fy > float64(t.H)-0.5 {
		return 0, false
	}
	fx = math.Max(0, math.Min(fx, float64(t.W-1)))
	fy = math.Max(0, math.Min(fy, float64(t.H-1)))
	x0, y0 := int(fx), int(fy)
	x1, y1 := x0+1, y0+1
	if x1 >= t.W {
		x1 = x0
	}
	if y1 >= t.H {
		y1 = y0
	}
	dx, dy := fx-float64(x0), fy-float64(y0)
	var sum, wsum float64
	for _, p := range [4]struct {
		x, y int
		w    float64
	}{{x0, y0, (1 - dx) * (1 - dy)}, {x1, y0, dx * (1 - dy)}, {x0, y1, (1 - dx) * dy}, {x1, y1, dx * dy}} {
		v := float64(t.Data[p.y*t.W+p.x])
		if p.w == 0 || math.IsNaN(v) || (t.HasNoData && v == t.NoData) || v < -1000 {
			continue
		}
		sum += v * p.w
		wsum += p.w
	}
	if wsum < 0.25 {
		return 0, false
	}
	return sum / wsum, true
}
