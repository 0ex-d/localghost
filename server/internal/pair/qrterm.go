package pair

import (
	"os"
	"strconv"
	"strings"
)

// TerminalCols reports the usable terminal width for QR rendering: the TIOCGWINSZ ioctl on stdout
// when stdout is a terminal, the COLUMNS environment variable otherwise (shells export it in
// interactive sessions; systemd units and pipes usually do not), and 0 when neither is available so
// the caller can apply its own default.
func TerminalCols() int {
	if c := terminalCols(); c > 0 {
		return c
	}
	if s := os.Getenv("COLUMNS"); s != "" {
		if c, err := strconv.Atoi(s); err == nil && c > 0 {
			return c
		}
	}
	return 0
}

// Matrix is a square QR grid. Dark(x, y) is true for a dark module. This is the seam: any QR
// encoder that can report dark modules plugs in here. The renderer below is independent of how the
// matrix was produced.
type Matrix interface {
	Size() int
	Dark(x, y int) bool
}

// RenderTerminal draws the QR using Unicode half-block characters, two modules per character cell
// so it is not stretched 2:1. Colours are set explicitly via ANSI (black on white) so it scans
// regardless of the terminal theme, and a 4-module quiet zone is added, which scanners require.
//
// Per cell pair (top, bottom):
//
//	dark,dark  -> full block, dark,light -> upper half, light,dark -> lower half, light,light -> space
func RenderTerminal(m Matrix) string {
	const (
		reset = "\x1b[0m"
		// white background, black foreground: theme-independent, dark modules read as black.
		colour = "\x1b[47;30m"
		full   = "\u2588" // both halves dark
		upper  = "\u2580" // top dark
		lower  = "\u2584" // bottom dark
		blank  = " "      // both light
	)
	const quiet = 4
	n := m.Size()

	dark := func(x, y int) bool {
		// Outside the matrix is the quiet zone: always light.
		if x < 0 || y < 0 || x >= n || y >= n {
			return false
		}
		return m.Dark(x, y)
	}

	var b strings.Builder
	// Step two rows at a time over the matrix plus quiet zone on every side.
	for y := -quiet; y < n+quiet; y += 2 {
		b.WriteString(colour)
		for x := -quiet; x < n+quiet; x++ {
			top := dark(x, y)
			bottom := dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteString(full)
			case top && !bottom:
				b.WriteString(upper)
			case !top && bottom:
				b.WriteString(lower)
			default:
				b.WriteString(blank)
			}
		}
		b.WriteString(reset)
		b.WriteString("\n")
	}
	return b.String()
}

// quadGlyphs maps the four sub-modules of one character cell to a quadrant block glyph. Index is
// (UL<<3)|(UR<<2)|(LL<<1)|LR where each bit is 1 for a dark module.
var quadGlyphs = [16]string{
	" ",        // ....
	"\u2597",   // ...LR
	"\u2596",   // ..LL.
	"\u2584",   // ..LL LR (lower half)
	"\u259D",   // .UR..
	"\u2590",   // .UR.LR (right half)
	"\u259E",   // .UR LL.
	"\u259F",   // .UR LL LR
	"\u2598",   // UL...
	"\u259A",   // UL..LR
	"\u258C",   // UL.LL. (left half)
	"\u2599",   // UL.LL LR
	"\u2580",   // UL UR.. (upper half)
	"\u259C",   // UL UR.LR
	"\u259B",   // UL UR LL.
	"\u2588",   // all four (full block)
}

// RenderTerminalQuad draws the QR at 2x2 modules per character cell using Unicode quadrant blocks,
// half the width of RenderTerminal. A v15 symbol (77 modules + 8 quiet) is 85 half-block columns,
// which clips on an 80-column terminal, and a clipped QR does not scan; at 2x2 it is 43 columns.
// The trade-off, stated plainly: quadrant glyphs (U+2596..U+259F) are missing from some raw Linux
// console fonts (the half blocks and full block are not), so on a bare tty this can render as
// replacement boxes. RenderTerminalFit therefore prefers half-block whenever it fits and only drops
// to quadrants when the alternative is a clipped, unscannable symbol.
func RenderTerminalQuad(m Matrix) string {
	const (
		reset  = "\x1b[0m"
		colour = "\x1b[47;30m"
	)
	const quiet = 4
	n := m.Size()

	dark := func(x, y int) bool {
		if x < 0 || y < 0 || x >= n || y >= n {
			return false
		}
		return m.Dark(x, y)
	}

	var b strings.Builder
	for y := -quiet; y < n+quiet; y += 2 {
		b.WriteString(colour)
		for x := -quiet; x < n+quiet; x += 2 {
			idx := 0
			if dark(x, y) {
				idx |= 8
			}
			if dark(x+1, y) {
				idx |= 4
			}
			if dark(x, y+1) {
				idx |= 2
			}
			if dark(x+1, y+1) {
				idx |= 1
			}
			b.WriteString(quadGlyphs[idx])
		}
		b.WriteString(reset)
		b.WriteString("\n")
	}
	return b.String()
}

// RenderTerminalFit picks the largest rendering that fits in cols columns: half-block (one module
// per column, easiest to scan) when it fits, quadrant blocks (two modules per column) otherwise.
// cols <= 0 means the width is unknown; assume a standard 80 columns rather than guessing wide.
// Returns the rendering plus the number of columns it needs, so the caller can tell the user to
// widen the terminal when even the quadrant form does not fit instead of printing a clipped symbol.
func RenderTerminalFit(m Matrix, cols int) (rendered string, needCols int) {
	const quiet = 4
	if cols <= 0 {
		cols = 80
	}
	span := m.Size() + 2*quiet
	if span <= cols {
		return RenderTerminal(m), span
	}
	need := (span + 1) / 2
	return RenderTerminalQuad(m), need
}
