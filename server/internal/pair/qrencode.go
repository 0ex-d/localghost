package pair

import (
	"errors"
)

// From-scratch QR encoder: byte mode, versions 1-20, EC level M with a level-L fallback for
// payloads above M's ceiling. Replaces rsc.io/qr so LocalGhost
// carries no third-party encoding code. The enrollment payload was short ASCII; embedding the client
// certificate in the enroll link pushed it to ~400 bytes, which needs version 15, so the range now
// runs to version 20 (669 data codewords at M, 666 payload bytes after mode and count bits).
// Versions 7 and up carry the two 18-module version
// information blocks the spec requires; earlier revisions of this encoder omitted them, which made
// every v7-10 symbol non-standard (the app's decoder reserves those modules, so the bit streams
// misaligned and nothing above v6 scanned). Validated by round-trip against an independently written
// decoder across payload sizes, plus golden version-information constants taken from the spec.
//
// A bad encoder fails safe at this seam: the QR just will not scan and you fall back to typing the
// three values. Mask selection uses the full penalty scoring so the symbol is robust, not just
// usually-readable.

// GF(256), primitive 0x11D.
var (
	gfExp [512]int
	gfLog [256]int
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = x
		gfLog[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

// versionM[v] = {totalDataCodewords, ecPerBlock, g1blocks, g1dataCW, g2blocks, g2dataCW} at level M.
var versionM = map[int][6]int{
	1:  {16, 10, 1, 16, 0, 0},
	2:  {28, 16, 1, 28, 0, 0},
	3:  {44, 26, 1, 44, 0, 0},
	4:  {64, 18, 2, 32, 0, 0},
	5:  {86, 24, 2, 43, 0, 0},
	6:  {108, 16, 4, 27, 0, 0},
	7:  {124, 18, 4, 31, 0, 0},
	8:  {154, 22, 2, 38, 2, 39},
	9:  {182, 22, 3, 36, 2, 37},
	10: {216, 26, 4, 43, 1, 44},
	11: {254, 30, 1, 50, 4, 51},
	12: {290, 22, 6, 36, 2, 37},
	13: {334, 22, 8, 37, 1, 38},
	14: {365, 24, 4, 40, 5, 41},
	15: {415, 24, 5, 41, 5, 42},
	16: {453, 28, 7, 45, 3, 46},
	17: {507, 28, 10, 46, 1, 47},
	18: {563, 26, 9, 43, 4, 44},
	19: {627, 26, 3, 44, 11, 45},
	20: {669, 26, 3, 41, 13, 42},
}

// versionL[v] = same layout at EC level L. Level M is the default for its stronger correction; L
// exists ONLY as the fallback for payloads above M's 666-byte ceiling (a DER cert + key + link is
// ~700-800 bytes). The enrolment QR is terminal-rendered , flat, high-contrast, no logo, no print
// damage , which is exactly the regime where L's weaker correction costs nothing. Totals verified
// against symbol codeword counts (e.g. v15: 523 data + 6x22 EC = 655; v20: 861 + 8x28 = 1085).
var versionL = map[int][6]int{
	1:  {19, 7, 1, 19, 0, 0},
	2:  {34, 10, 1, 34, 0, 0},
	3:  {55, 15, 1, 55, 0, 0},
	4:  {80, 20, 1, 80, 0, 0},
	5:  {108, 26, 1, 108, 0, 0},
	6:  {136, 18, 2, 68, 0, 0},
	7:  {156, 20, 2, 78, 0, 0},
	8:  {194, 24, 2, 97, 0, 0},
	9:  {232, 30, 2, 116, 0, 0},
	10: {274, 18, 2, 68, 2, 69},
	11: {324, 20, 4, 81, 0, 0},
	12: {370, 24, 2, 92, 2, 93},
	13: {428, 26, 4, 107, 0, 0},
	14: {461, 30, 3, 115, 1, 116},
	15: {523, 22, 5, 87, 1, 88},
	16: {589, 24, 5, 98, 1, 99},
	17: {647, 28, 1, 107, 5, 108},
	18: {721, 30, 5, 120, 1, 121},
	19: {795, 28, 3, 113, 4, 114},
	20: {861, 28, 3, 107, 5, 108},
}

// EC levels this encoder emits. The 2-bit values are the format-information encoding from the spec
// (M=00, L=01); the app's decoder reads the level from the format bits of any standard symbol, so
// no app change is needed when a symbol arrives at L.
const (
	levelM byte = 0b00
	levelL byte = 0b01
)

func versionTable(level byte) map[int][6]int {
	if level == levelL {
		return versionL
	}
	return versionM
}

// alignCenters computes the alignment-pattern centre coordinates from the spec's placement rule
// rather than tabulating them, the same derivation the app's decoder uses, so both codebases agree
// by construction and there is no 40-row table to mistype. Reproduces the previous hand-written
// table exactly for v2-10 (asserted in tests). The v32 special case is kept for completeness even
// though this encoder stops at v20.
func alignCenters(v int) []int {
	if v == 1 {
		return nil
	}
	count := v/7 + 2
	step := (v*4 + count*2 + 1) / (2*count - 2) * 2
	if v == 32 {
		step = 26
	}
	last := 17 + 4*v - 7
	pos := make([]int, count)
	pos[0] = 6
	for j := 1; j < count; j++ {
		pos[j] = last - (count-1-j)*step
	}
	return pos
}

func qrSide(v int) int { return 17 + 4*v }

var ErrPayloadTooBig = errors.New("payload too big for QR versions 1-20 (666 bytes at level M, 858 at the level-L fallback)")

// chooseVersion picks the smallest symbol: every payload that fits level M uses M (stronger
// correction, unchanged behaviour for everything that fit before); only payloads above M's ceiling
// fall back to level L, whose v20 holds 858 bytes.
func chooseVersion(nbytes int) (int, byte, error) {
	for _, level := range []byte{levelM, levelL} {
		table := versionTable(level)
		for v := 1; v <= 20; v++ {
			ccbits := 8
			if v >= 10 {
				ccbits = 16
			}
			capBits := table[v][0] * 8
			need := 4 + ccbits + nbytes*8
			if need <= capBits {
				return v, level, nil
			}
		}
	}
	return 0, 0, ErrPayloadTooBig
}

func rsGen(n int) []int {
	g := []int{1}
	for i := 0; i < n; i++ {
		ng := make([]int, len(g)+1)
		for j := 0; j < len(g); j++ {
			ng[j] ^= g[j]
			ng[j+1] ^= gfMul(g[j], gfExp[i])
		}
		g = ng
	}
	return g
}

func rsEC(data []int, n int) []int {
	gen := rsGen(n)
	res := make([]int, len(data)+n)
	copy(res, data)
	for i := 0; i < len(data); i++ {
		c := res[i]
		if c != 0 {
			for j := 0; j < len(gen); j++ {
				res[i+j] ^= gfMul(gen[j], c)
			}
		}
	}
	return res[len(data):]
}

func encodeData(text string, v int, level byte) []int {
	cfg := versionTable(level)[v]
	total, ecpb, g1b, g1d, g2b, g2d := cfg[0], cfg[1], cfg[2], cfg[3], cfg[4], cfg[5]
	data := []byte(text)
	ccbits := 8
	if v >= 10 {
		ccbits = 16
	}
	var bits []int
	put := func(val, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, (val>>i)&1)
		}
	}
	put(0b0100, 4) // byte mode
	put(len(data), ccbits)
	for _, b := range data {
		put(int(b), 8)
	}
	put(0, 4) // terminator
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	var cws []int
	for i := 0; i < len(bits); i += 8 {
		val := 0
		for j := 0; j < 8; j++ {
			val = val<<1 | bits[i+j]
		}
		cws = append(cws, val)
	}
	pad := []int{0xEC, 0x11}
	for k := 0; len(cws) < total; k++ {
		cws = append(cws, pad[k%2])
	}
	// split into blocks
	var blocks [][]int
	idx := 0
	for i := 0; i < g1b; i++ {
		blocks = append(blocks, cws[idx:idx+g1d])
		idx += g1d
	}
	for i := 0; i < g2b; i++ {
		blocks = append(blocks, cws[idx:idx+g2d])
		idx += g2d
	}
	ecs := make([][]int, len(blocks))
	for i, b := range blocks {
		ecs[i] = rsEC(b, ecpb)
	}
	// interleave data then ec
	var final []int
	maxd := 0
	for _, b := range blocks {
		if len(b) > maxd {
			maxd = len(b)
		}
	}
	for i := 0; i < maxd; i++ {
		for _, b := range blocks {
			if i < len(b) {
				final = append(final, b[i])
			}
		}
	}
	maxe := 0
	for _, e := range ecs {
		if len(e) > maxe {
			maxe = len(e)
		}
	}
	for i := 0; i < maxe; i++ {
		for _, e := range ecs {
			if i < len(e) {
				final = append(final, e[i])
			}
		}
	}
	return final
}

// ---- matrix construction ----

type grid struct {
	m   [][]int  // module values (0/1)
	res [][]bool // reserved (function pattern) modules
	n   int
}

func newGrid(v int) *grid {
	n := qrSide(v)
	m := make([][]int, n)
	res := make([][]bool, n)
	for i := range m {
		m[i] = make([]int, n)
		res[i] = make([]bool, n)
	}
	return &grid{m: m, res: res, n: n}
}

func (g *grid) placeFunctionPatterns(v int) {
	n := g.n
	finder := func(r, c int) {
		for dr := -1; dr <= 7; dr++ {
			for dc := -1; dc <= 7; dc++ {
				rr, cc := r+dr, c+dc
				if rr < 0 || rr >= n || cc < 0 || cc >= n {
					continue
				}
				g.res[rr][cc] = true
				if dr >= 0 && dr < 7 && dc >= 0 && dc < 7 {
					on := dr == 0 || dr == 6 || dc == 0 || dc == 6 || (dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4)
					if on {
						g.m[rr][cc] = 1
					} else {
						g.m[rr][cc] = 0
					}
				} else {
					g.m[rr][cc] = 0
				}
			}
		}
	}
	finder(0, 0)
	finder(0, n-7)
	finder(n-7, 0)
	// timing
	for i := 8; i < n-8; i++ {
		v0 := 0
		if i%2 == 0 {
			v0 = 1
		}
		g.m[6][i] = v0
		g.res[6][i] = true
		g.m[i][6] = v0
		g.res[i][6] = true
	}
	// dark module
	g.m[4*v+9][8] = 1
	g.res[4*v+9][8] = true
	// alignment
	centers := alignCenters(v)
	for _, r := range centers {
		for _, c := range centers {
			if (r <= 8 && c <= 8) || (r <= 8 && c >= n-9) || (r >= n-9 && c <= 8) {
				continue
			}
			for dr := -2; dr <= 2; dr++ {
				for dc := -2; dc <= 2; dc++ {
					rr, cc := r+dr, c+dc
					g.res[rr][cc] = true
					on := abs(dr) == 2 || abs(dc) == 2 || (dr == 0 && dc == 0)
					if on {
						g.m[rr][cc] = 1
					} else {
						g.m[rr][cc] = 0
					}
				}
			}
		}
	}
	// reserve format areas
	for i := 0; i <= 8; i++ {
		g.res[8][i] = true
		g.res[i][8] = true
	}
	for i := 0; i < 8; i++ {
		g.res[8][n-1-i] = true
		g.res[n-1-i][8] = true
	}
	// Version information (v >= 7): two 18-module blocks carrying the BCH(18,6)-coded version, one
	// just left of the top-right finder (rows 0..5, cols n-11..n-9) and one just above the bottom-left
	// finder (rows n-11..n-9, cols 0..5). Bit i (LSB first) sits at (i/3, n-11+i%3) and its transpose.
	// These are fixed per version, independent of the mask, so they are written once here and reserved;
	// masked() leaves reserved modules alone. Omitting them (as this encoder originally did) makes a
	// v7+ symbol non-standard: a spec decoder skips these 36 modules when reading data, so every data
	// bit after them lands one position off and Reed-Solomon fails on the whole symbol.
	if v >= 7 {
		vb := versionBits(v)
		for i := 0; i < 18; i++ {
			bit := (vb >> i) & 1
			r1, c1 := i/3, n-11+i%3
			g.m[r1][c1] = bit
			g.res[r1][c1] = true
			g.m[c1][r1] = bit
			g.res[c1][r1] = true
		}
	}
}

// versionBits computes the 18-bit version information: the 6-bit version number followed by its
// 12-bit BCH(18,6) remainder, generator polynomial 0x1F25 (spec Annex D; the worked example there is
// v7 = 0x07C94, asserted in tests along with the value read from a known-good third-party symbol).
func versionBits(v int) int {
	d := v << 12
	g := 0x1F25
	for i := 17; i >= 12; i-- {
		if (d>>i)&1 == 1 {
			d ^= g << (i - 12)
		}
	}
	return (v << 12) | d
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (g *grid) placeData(codewords []int) {
	n := g.n
	var bits []int
	for _, cw := range codewords {
		for i := 7; i >= 0; i-- {
			bits = append(bits, (cw>>i)&1)
		}
	}
	idx := 0
	col := n - 1
	upward := true
	for col > 0 {
		if col == 6 {
			col--
		}
		for i := 0; i < n; i++ {
			r := i
			if upward {
				r = n - 1 - i
			}
			for _, c := range []int{col, col - 1} {
				if !g.res[r][c] {
					b := 0
					if idx < len(bits) {
						b = bits[idx]
					}
					g.m[r][c] = b
					idx++
				}
			}
		}
		col -= 2
		upward = !upward
	}
}

var maskFns = []func(r, c int) bool{
	func(r, c int) bool { return (r+c)%2 == 0 },
	func(r, c int) bool { return r%2 == 0 },
	func(r, c int) bool { return c%3 == 0 },
	func(r, c int) bool { return (r+c)%3 == 0 },
	func(r, c int) bool { return (r/2+c/3)%2 == 0 },
	func(r, c int) bool { return (r*c)%2+(r*c)%3 == 0 },
	func(r, c int) bool { return ((r*c)%2+(r*c)%3)%2 == 0 },
	func(r, c int) bool { return ((r+c)%2+(r*c)%3)%2 == 0 },
}

func (g *grid) masked(mask int) [][]int {
	out := make([][]int, g.n)
	for r := 0; r < g.n; r++ {
		out[r] = make([]int, g.n)
		copy(out[r], g.m[r])
		for c := 0; c < g.n; c++ {
			if !g.res[r][c] && maskFns[mask](r, c) {
				out[r][c] ^= 1
			}
		}
	}
	return out
}

func penalty(m [][]int, n int) int {
	score := 0
	// rule 1: runs of 5+ (rows + cols)
	lines := make([][]int, 0, 2*n)
	for r := 0; r < n; r++ {
		lines = append(lines, m[r])
	}
	for c := 0; c < n; c++ {
		col := make([]int, n)
		for r := 0; r < n; r++ {
			col[r] = m[r][c]
		}
		lines = append(lines, col)
	}
	for _, line := range lines {
		run := 1
		for i := 1; i < n; i++ {
			if line[i] == line[i-1] {
				run++
			} else {
				if run >= 5 {
					score += 3 + (run - 5)
				}
				run = 1
			}
		}
		if run >= 5 {
			score += 3 + (run - 5)
		}
	}
	// rule 2: 2x2 blocks
	for r := 0; r < n-1; r++ {
		for c := 0; c < n-1; c++ {
			if m[r][c] == m[r][c+1] && m[r][c] == m[r+1][c] && m[r][c] == m[r+1][c+1] {
				score += 3
			}
		}
	}
	// rule 3: finder-like 11-module patterns
	pat1 := []int{1, 0, 1, 1, 1, 0, 1, 0, 0, 0, 0}
	pat2 := []int{0, 0, 0, 0, 1, 0, 1, 1, 1, 0, 1}
	match := func(seg []int) bool { return eq(seg, pat1) || eq(seg, pat2) }
	for r := 0; r < n; r++ {
		for c := 0; c <= n-11; c++ {
			seg := m[r][c : c+11]
			if match(seg) {
				score += 40
			}
		}
	}
	for c := 0; c < n; c++ {
		for r := 0; r <= n-11; r++ {
			seg := make([]int, 11)
			for k := 0; k < 11; k++ {
				seg[k] = m[r+k][c]
			}
			if match(seg) {
				score += 40
			}
		}
	}
	// rule 4: dark proportion
	dark := 0
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			dark += m[r][c]
		}
	}
	pct := dark * 100 / (n * n)
	d := pct - 50
	if d < 0 {
		d = -d
	}
	score += 10 * (d / 5)
	return score
}

func eq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// formatBits computes the 15-bit format information (BCH(15,5) + mask 0x5412) for EC level and mask.
func formatBits(ecLevel, mask int) []int {
	data := (ecLevel << 3) | mask
	v := data << 10
	g := 0x537
	for i := 4; i >= 0; i-- {
		if (v>>(10+i))&1 == 1 {
			v ^= g << i
		}
	}
	bits := ((data << 10) | (v & 0x3FF)) ^ 0x5412
	out := make([]int, 15)
	for i := 0; i < 15; i++ {
		out[i] = (bits >> (14 - i)) & 1
	}
	return out
}

func placeFormat(m [][]int, n, mask int, level byte) {
	fb := formatBits(int(level), mask) // M = 00, L = 01
	c1 := [][2]int{{8, 0}, {8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5}, {8, 7}, {8, 8}, {7, 8}, {5, 8}, {4, 8}, {3, 8}, {2, 8}, {1, 8}, {0, 8}}
	for i, rc := range c1 {
		m[rc[0]][rc[1]] = fb[i]
	}
	c2 := [][2]int{{n - 1, 8}, {n - 2, 8}, {n - 3, 8}, {n - 4, 8}, {n - 5, 8}, {n - 6, 8}, {n - 7, 8},
		{8, n - 8}, {8, n - 7}, {8, n - 6}, {8, n - 5}, {8, n - 4}, {8, n - 3}, {8, n - 2}, {8, n - 1}}
	for i, rc := range c2 {
		m[rc[0]][rc[1]] = fb[i]
	}
}

// qrMatrix is the encoded symbol implementing the Matrix seam.
type qrMatrix struct {
	m [][]int
	n int
}

func (q qrMatrix) Size() int          { return q.n }
func (q qrMatrix) Dark(x, y int) bool { return q.m[y][x] == 1 }

// EncodeQR encodes text into a QR symbol (byte mode, versions 1-20, level M or the level-L
// fallback for payloads above 666 bytes) with full mask selection, returning the Matrix the
// terminal renderer draws.
func EncodeQR(text string) (Matrix, error) {
	v, level, err := chooseVersion(len([]byte(text)))
	if err != nil {
		return nil, err
	}
	cws := encodeData(text, v, level)
	g := newGrid(v)
	g.placeFunctionPatterns(v)
	g.placeData(cws)
	bestScore := -1
	var best [][]int
	for mask := 0; mask < 8; mask++ {
		cand := g.masked(mask)
		placeFormat(cand, g.n, mask, level)
		sc := penalty(cand, g.n)
		if bestScore < 0 || sc < bestScore {
			bestScore = sc
			best = cand
		}
	}
	return qrMatrix{m: best, n: g.n}, nil
}

