package pair

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// roundTripDecode is an independently-written decoder used ONLY in tests to prove the encoder
// produces a spec-correct symbol: encode text -> matrix -> decode back -> must equal the input. A
// scanner reads a symbol the same way, so a clean round-trip is strong evidence it will scan.
func roundTripDecode(q qrMatrix, v, mask int, level byte) (string, error) {
	g := newGrid(v)
	g.placeFunctionPatterns(v)
	n := q.n
	// unmask
	um := make([][]int, n)
	for r := 0; r < n; r++ {
		um[r] = make([]int, n)
		copy(um[r], q.m[r])
		for c := 0; c < n; c++ {
			if !g.res[r][c] && maskFns[mask](r, c) {
				um[r][c] ^= 1
			}
		}
	}
	// read codewords in the same zigzag
	var bits []int
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
					bits = append(bits, um[r][c])
				}
			}
		}
		col -= 2
		upward = !upward
	}
	var cws []int
	for i := 0; i+8 <= len(bits); i += 8 {
		val := 0
		for j := 0; j < 8; j++ {
			val = val<<1 | bits[i+j]
		}
		cws = append(cws, val)
	}
	// de-interleave back to block order, take data codewords
	cfg := versionTable(level)[v]
	g1b, g1d, g2b, g2d := cfg[2], cfg[3], cfg[4], cfg[5]
	sizes := []int{}
	for i := 0; i < g1b; i++ {
		sizes = append(sizes, g1d)
	}
	for i := 0; i < g2b; i++ {
		sizes = append(sizes, g2d)
	}
	blocks := make([][]int, len(sizes))
	idx := 0
	maxd := 0
	for _, s := range sizes {
		if s > maxd {
			maxd = s
		}
	}
	for i := 0; i < maxd; i++ {
		for bi, s := range sizes {
			if i < s {
				blocks[bi] = append(blocks[bi], cws[idx])
				idx++
			}
		}
	}
	var data []int
	for _, b := range blocks {
		data = append(data, b...)
	}
	// parse byte mode
	var dbits []int
	for _, cw := range data {
		for i := 7; i >= 0; i-- {
			dbits = append(dbits, (cw>>i)&1)
		}
	}
	pos := 0
	take := func(k int) int {
		val := 0
		for i := 0; i < k; i++ {
			val = val<<1 | dbits[pos]
			pos++
		}
		return val
	}
	if mode := take(4); mode != 0b0100 {
		return "", fmt.Errorf("unexpected mode %d", mode)
	}
	ccbits := 8
	if v >= 10 {
		ccbits = 16
	}
	length := take(ccbits)
	out := make([]byte, length)
	for i := 0; i < length; i++ {
		out[i] = byte(take(8))
	}
	return string(out), nil
}

// encodeForTest re-runs EncodeQR but also reports the chosen version+mask+level for the decoder.
func encodeForTest(t *testing.T, text string) (qrMatrix, int, int, byte) {
	t.Helper()
	v, level, err := chooseVersion(len([]byte(text)))
	if err != nil {
		t.Fatal(err)
	}
	cws := encodeData(text, v, level)
	g := newGrid(v)
	g.placeFunctionPatterns(v)
	g.placeData(cws)
	bestScore := -1
	var best [][]int
	bestMask := 0
	for mask := 0; mask < 8; mask++ {
		cand := g.masked(mask)
		placeFormat(cand, g.n, mask, level)
		sc := penalty(cand, g.n)
		if bestScore < 0 || sc < bestScore {
			bestScore = sc
			best = cand
			bestMask = mask
		}
	}
	return qrMatrix{m: best, n: g.n}, v, bestMask, level
}

func TestQRRoundTrip(t *testing.T) {
	// The real link the box emits, built through EnrollLink.String() so the test exercises exactly
	// what ships (including v= and the base64url DER cert+key), not a hand-written approximation.
	// The DER sizes match a real minimal P-256 identity (~380-byte cert, 138-byte PKCS8 key), which
	// puts the whole link around 790 bytes: above level M's 666, inside level L's 858, so this also
	// exercises the L fallback on the genuine payload shape.
	realLink := EnrollLink{
		Host: "192.168.1.50", Port: 8443,
		Fingerprint: "AB:12:CD:34", BoxName: "xyntai",
		DeviceCertDER: testCertDER, DeviceKeyDER: testKeyDER,
	}.String()

	payloads := []string{
		realLink,
		"localghost://enroll?host=192.168.1.50&port=8443&fp=AB12CD34&name=xyntai",
		"localghost://enroll?host=10.0.0.5&fp=AA",
		"hello",
		"a",
	}
	// add varying sizes: into every block-structure change up to the v20 ceiling. 213 is the v10
	// boundary, 400 is the real cert-bearing enroll link size (v15), 669 is the exact v20 capacity.
	for _, nc := range []int{16, 40, 80, 120, 160, 200, 213, 260, 300, 400, 500, 600, 666, 700, 800, 858} {
		s := ""
		for i := 0; i < nc; i++ {
			s += string(rune('A' + (i % 26)))
		}
		payloads = append(payloads, s)
	}
	for _, p := range payloads {
		q, v, mask, level := encodeForTest(t, p)
		back, err := roundTripDecode(q, v, mask, level)
		if err != nil {
			t.Fatalf("decode of %d-char payload failed: %v", len(p), err)
		}
		if back != p {
			t.Fatalf("round-trip mismatch (len %d, v%d): got %q", len(p), v, back)
		}
	}
}

func TestQRMatrixIsSquareBinary(t *testing.T) {
	m, err := EncodeQR("localghost://enroll?host=10.0.0.1&code=AB&fp=CD")
	if err != nil {
		t.Fatal(err)
	}
	n := m.Size()
	if n < 21 {
		t.Fatalf("a v1 symbol is at least 21 modules, got %d", n)
	}
	// Dark() must be callable for every cell.
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			_ = m.Dark(x, y)
		}
	}
}

func TestQRTooBigFails(t *testing.T) {
	big := make([]byte, 1000)
	for i := range big {
		big[i] = 'A'
	}
	if _, err := EncodeQR(string(big)); err != ErrPayloadTooBig {
		t.Fatalf("oversized payload must fail with ErrPayloadTooBig, got %v", err)
	}
	// Exact boundaries, probed rather than trusted: 666 bytes is the last payload that fits level M
	// (669 data codewords minus mode+count bits); 667 must silently fall back to level L; 858 is the
	// last that fits at all (861 L codewords at v20); 859 must fail.
	fitsM := 0
	for n := 660; n <= 680; n++ {
		if _, lvl, err := chooseVersion(n); err == nil && lvl == levelM {
			fitsM = n
		}
	}
	if fitsM != 666 {
		t.Fatalf("largest level-M payload = %d, want 666", fitsM)
	}
	if _, lvl, err := chooseVersion(667); err != nil || lvl != levelL {
		t.Fatalf("667 bytes must fall back to level L, got level %d err %v", lvl, err)
	}
	fitsL := 0
	for n := 850; n <= 870; n++ {
		if _, _, err := chooseVersion(n); err == nil {
			fitsL = n
		}
	}
	if fitsL != 858 {
		t.Fatalf("largest payload overall = %d, want 858 (level L, v20)", fitsL)
	}
}

// TestQRVersionInfoGolden pins the 18-bit version-information words against constants computed
// straight from the spec's BCH(18,6) definition, NOT against this encoder's own arithmetic: v7 is
// the worked example in the spec's annex, and v15 (0x0F928) was additionally verified against the
// version blocks physically present in a known-good symbol from an independent generator. A shared
// bug between the encoder and the test decoder cannot slip past constants that came from outside.
func TestQRVersionInfoGolden(t *testing.T) {
	golden := map[int]int{
		7: 0x07C94, 8: 0x085BC, 9: 0x09A99, 10: 0x0A4D3, 11: 0x0BBF6,
		12: 0x0C762, 13: 0x0D847, 14: 0x0E60D, 15: 0x0F928, 16: 0x10B78,
		17: 0x1145D, 18: 0x12A17, 19: 0x13532, 20: 0x149A6,
	}
	for v, want := range golden {
		if got := versionBits(v); got != want {
			t.Fatalf("versionBits(%d) = %#05X, want %#05X", v, got, want)
		}
	}
}

// TestQRVersionInfoPlacement encodes a payload that lands on a v>=7 symbol and reads the two
// version blocks back off the matrix at the spec's module positions: bit i at (i/3, n-11+i%3) top
// right, transposed bottom left. This is what an external scanner reads, so it must be present and
// correct on the final masked symbol, not just in the encoder's internal grid.
func TestQRVersionInfoPlacement(t *testing.T) {
	q, v, _, _ := encodeForTest(t, strings.Repeat("A", 400)) // 400 bytes -> v15 at level M
	if v < 7 {
		t.Fatalf("payload chose v%d, need v>=7 for this test", v)
	}
	n := q.n
	want := versionBits(v)
	var tr, bl int
	for i := 0; i < 18; i++ {
		if q.m[i/3][n-11+i%3] == 1 {
			tr |= 1 << i
		}
		if q.m[n-11+i%3][i/3] == 1 {
			bl |= 1 << i
		}
	}
	if tr != want || bl != want {
		t.Fatalf("version blocks on final symbol: top-right %#05X bottom-left %#05X, want %#05X", tr, bl, want)
	}
}

// TestAlignCentersMatchesOldTable pins the computed placement rule to the hand-written table it
// replaced for v2-10 (so nothing shipped changes) and to spec values for the new range, including
// v15's {6,26,48,70} verified against a real independently-generated symbol.
func TestAlignCentersMatchesOldTable(t *testing.T) {
	want := map[int][]int{
		1: {}, 2: {6, 18}, 3: {6, 22}, 4: {6, 26}, 5: {6, 30}, 6: {6, 34},
		7: {6, 22, 38}, 8: {6, 24, 42}, 9: {6, 26, 46}, 10: {6, 28, 50},
		11: {6, 30, 54}, 12: {6, 32, 58}, 13: {6, 34, 62}, 14: {6, 26, 46, 66},
		15: {6, 26, 48, 70}, 16: {6, 26, 50, 74}, 17: {6, 30, 54, 78},
		18: {6, 30, 56, 82}, 19: {6, 30, 58, 86}, 20: {6, 34, 62, 90},
	}
	for v, w := range want {
		got := alignCenters(v)
		if len(got) != len(w) {
			t.Fatalf("alignCenters(%d) = %v, want %v", v, got, w)
		}
		for i := range w {
			if got[i] != w[i] {
				t.Fatalf("alignCenters(%d) = %v, want %v", v, got, w)
			}
		}
	}
}

// TestRenderTerminalFit checks the width decision: half-block when the symbol plus quiet zone fits,
// quadrant otherwise, and that the reported need matches what was rendered (first line's module
// span). A v15 symbol is 85 half-block columns, so on 80 it must drop to quadrants at 43.
func TestRenderTerminalFit(t *testing.T) {
	q, v, _, _ := encodeForTest(t, strings.Repeat("A", 400))
	if v != 15 {
		t.Fatalf("expected v15 for a 400-byte payload, got v%d", v)
	}
	if _, need := RenderTerminalFit(q, 120); need != 85 {
		t.Fatalf("wide terminal: need = %d, want 85 (half-block)", need)
	}
	if _, need := RenderTerminalFit(q, 80); need != 43 {
		t.Fatalf("80-column terminal: need = %d, want 43 (quadrant)", need)
	}
	small, _, _, _ := encodeForTest(t, "localghost://enroll?host=10.0.0.5&fp=AA")
	if _, need := RenderTerminalFit(small, 80); need != small.n+8 {
		t.Fatalf("small symbol on 80 columns must stay half-block: need = %d, want %d", need, small.n+8)
	}
}

func TestEnrollLinkCarriesVersion(t *testing.T) {
	// The app reads v as the format version (absent = 1). The box must emit it so a future format
	// change lets a newer box tell an older app to update rather than mis-parsing. Pin that the
	// emitted link contains the current version, and that it equals the documented constant.
	// v1 is the first published format and includes cert+key; nothing older exists in the wild.
	if CurrentVersion != 1 {
		t.Fatalf("CurrentVersion changed to %d , update the app's EnrollLink.CURRENT_VERSION in lockstep", CurrentVersion)
	}
	link := EnrollLink{Host: "10.0.0.1", Port: 8443, Fingerprint: "CD"}.String()
	want := "v=1"
	if !strings.Contains(link, want) {
		t.Fatalf("enroll link missing version: got %q, want it to contain %q", link, want)
	}
}

// Deterministic test DER at real identity sizes: ~380 bytes for a minimal P-256 client cert, 138
// for its PKCS8 key. Not valid crypto material , the link layer treats DER as opaque bytes; the
// phone's DeviceCert is what validates it. Byte values cover the full range so the base64url leg
// is exercised on binary, not just ASCII.
func fillDER(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

var (
	testCertDER = fillDER(380)
	testKeyDER  = fillDER(138)
)

// TestEnrollLinkCertKeyRoundTrip pins the cert/key leg of the link contract: String() emits both as
// unpadded base64url of the raw DER (never percent-encoded, so the QR stays compact), Parse()
// recovers the exact bytes, and the whole real-size link fits the QR encoder via the level-L
// fallback. The empty case is pinned too: a link without a cert must not emit an empty cert=.
func TestEnrollLinkCertKeyRoundTrip(t *testing.T) {
	in := EnrollLink{
		Host: "192.168.1.20", Port: 8443,
		Fingerprint: "3A:7B:C1:0E", BoxName: "xyntai",
		DeviceCertDER: testCertDER, DeviceKeyDER: testKeyDER,
	}
	link := in.String()
	for _, want := range []string{"v=1", "cert=", "key="} {
		if !strings.Contains(link, want) {
			t.Fatalf("link missing %q: %q", want, link)
		}
	}
	if strings.Contains(link, "%") {
		t.Fatalf("cert/key must not be percent-encoded (base64url is URL-safe), got %q", link)
	}
	out, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse of own String failed: %v", err)
	}
	if !bytes.Equal(out.DeviceCertDER, testCertDER) || !bytes.Equal(out.DeviceKeyDER, testKeyDER) {
		t.Fatalf("cert/key DER did not round-trip")
	}
	// Real identity sizes overshoot level M and must land on the level-L fallback, not fail.
	v, level, err := chooseVersion(len(link))
	if err != nil {
		t.Fatalf("real-size link (%d bytes) must fit the encoder: %v", len(link), err)
	}
	if level != levelL {
		t.Fatalf("real-size link (%d bytes) should use the level-L fallback, got level %d v%d", len(link), level, v)
	}
	if _, err := EncodeQR(link); err != nil {
		t.Fatalf("EncodeQR of real-size link failed: %v", err)
	}

	bare := EnrollLink{Host: "h", Port: 8443, Fingerprint: "aa"}.String()
	if strings.Contains(bare, "cert=") || strings.Contains(bare, "key=") {
		t.Fatalf("bare link must omit cert/key entirely, got %q", bare)
	}
	back, err := Parse(bare)
	if err != nil {
		t.Fatalf("Parse of bare link failed: %v", err)
	}
	if len(back.DeviceCertDER) != 0 || len(back.DeviceKeyDER) != 0 {
		t.Fatalf("bare link must parse with empty cert/key")
	}
}
