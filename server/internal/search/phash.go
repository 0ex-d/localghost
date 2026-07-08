package search

// Perceptual hash for near-duplicate detection (spec: dHash, 64-bit, Hamming <= 6 = near-dup). Pure
// Go, ~40 lines as advertised: grayscale, downscale to 9x8 with the area-average scaler already in
// the codebase's spirit, then a horizontal gradient bit per pixel. Collapses photo bursts before
// captioning, which is the expensive step.

import (
	"image"
	"math/bits"
)

// DHash computes the 64-bit difference hash of an image.
func DHash(img image.Image) uint64 {
	const w, h = 9, 8
	g := grayDownscale(img, w, h)
	var out uint64
	bit := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w-1; x++ {
			if g[y*w+x] > g[y*w+x+1] {
				out |= 1 << uint(63-bit)
			}
			bit++
		}
	}
	return out
}

// Hamming is the bit distance between two hashes.
func Hamming(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// NearDup applies the spec threshold.
func NearDup(a, b uint64) bool { return Hamming(a, b) <= 6 }

// grayDownscale area-averages the source into a w*h luminance grid.
func grayDownscale(src image.Image, w, h int) []uint32 {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	out := make([]uint32, w*h)
	for dy := 0; dy < h; dy++ {
		y0 := sb.Min.Y + dy*sh/h
		y1 := sb.Min.Y + (dy+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < w; dx++ {
			x0 := sb.Min.X + dx*sw/w
			x1 := sb.Min.X + (dx+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sum, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, b, _ := src.At(x, y).RGBA()
					// integer luma, BT.601-ish weights
					sum += uint64(299*r+587*g+114*b) / 1000
					n++
				}
			}
			out[dy*w+dx] = uint32(sum / n)
		}
	}
	return out
}
