// Package framed is the engine of the photo pipeline daemon: intake of raw images from the phone,
// archive-untouched storage, preview generation, EXIF extraction, and the daily location path that
// turns photos plus watch points into a journal-ready day.
package framed

import (
	"image"
)

// downscale resizes src so its long edge is at most maxEdge, using area averaging , each destination
// pixel is the mean of the source box it covers. For DOWNSCALING (the only direction we ever go) area
// averaging is the correct filter: it integrates every source pixel exactly once, so it neither drops
// detail like nearest-neighbour nor rings like sharp kernels. Pure stdlib, no new dependency; a photo
// preview does not need a resampling library.
//
// Returns src unchanged if it already fits.
func downscale(src image.Image, maxEdge int) image.Image {
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()
	if w <= maxEdge && h <= maxEdge {
		return src
	}
	var dw, dh int
	if w >= h {
		dw = maxEdge
		dh = h * maxEdge / w
	} else {
		dh = maxEdge
		dw = w * maxEdge / h
	}
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	// For each destination pixel, average the source rectangle [x0,x1)x[y0,y1) it maps to. Integer box
	// bounds via fixed-point mapping; boxes tile the source exactly.
	for dy := 0; dy < dh; dy++ {
		y0 := sb.Min.Y + dy*h/dh
		y1 := sb.Min.Y + (dy+1)*h/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < dw; dx++ {
			x0 := sb.Min.X + dx*w/dw
			x1 := sb.Min.X + (dx+1)*w/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rs, gs, bs, as, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, b, a := src.At(x, y).RGBA() // 16-bit channels
					rs += uint64(r)
					gs += uint64(g)
					bs += uint64(b)
					as += uint64(a)
					n++
				}
			}
			o := dst.PixOffset(dx, dy)
			dst.Pix[o+0] = uint8(rs / n >> 8)
			dst.Pix[o+1] = uint8(gs / n >> 8)
			dst.Pix[o+2] = uint8(bs / n >> 8)
			dst.Pix[o+3] = uint8(as / n >> 8)
		}
	}
	return dst
}
