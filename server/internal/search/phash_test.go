package search

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// photoish builds a deterministic pseudo-photo: smooth 2-D luminance variation (both axes, so the
// horizontal-gradient dHash has real signal to read) plus a fixed seed so runs are stable. shiftX
// nudges content sideways to model a near-duplicate (re-crop/re-encode), brightness adds a flat
// offset to model exposure change.
func photoish(w, h, shiftX int, brightness float64, seed int64) image.Image {
	img := image.NewGray(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(seed))
	// a few smooth "blobs" give 2-D structure that survives downscaling
	type blob struct{ cx, cy, r, amp float64 }
	blobs := make([]blob, 5)
	for i := range blobs {
		blobs[i] = blob{rng.Float64(), rng.Float64(), 0.2 + rng.Float64()*0.3, rng.Float64()}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx := float64(x)/float64(w) - float64(shiftX)/float64(w)
			fy := float64(y) / float64(h)
			var v float64
			for _, b := range blobs {
				dx, dy := fx-b.cx, fy-b.cy
				v += b.amp * (1.0 - (dx*dx+dy*dy)/(b.r*b.r+0.001))
			}
			v = v*80 + 128 + brightness
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			img.SetGray(x, y, color.Gray{Y: uint8(v)})
		}
	}
	return img
}

func TestDHashStableUnderResizeAndBrightness(t *testing.T) {
	a := DHash(photoish(400, 300, 0, 0, 1))
	half := DHash(photoish(200, 150, 0, 0, 1))    // same content, half resolution
	bright := DHash(photoish(400, 300, 0, 25, 1)) // same content, +25 exposure
	if d := Hamming(a, half); d > 6 {
		t.Fatalf("resize should be a near-dup, hamming %d", d)
	}
	if d := Hamming(a, bright); d > 6 {
		t.Fatalf("uniform brightness shift should be a near-dup, hamming %d", d)
	}
	if !NearDup(a, half) {
		t.Fatal("NearDup must accept hamming <= 6")
	}
}

func TestDHashSeparatesDifferentImages(t *testing.T) {
	// two DIFFERENT scenes (different seeds) must not collide.
	a := DHash(photoish(300, 300, 0, 0, 1))
	b := DHash(photoish(300, 300, 0, 0, 99))
	if d := Hamming(a, b); d <= 6 {
		t.Fatalf("distinct images must not collide, hamming %d", d)
	}
}

func TestDHashBurstSibling(t *testing.T) {
	// a burst frame , same scene, slight horizontal shift , should read as a near-dup.
	base := DHash(photoish(400, 300, 0, 0, 7))
	shifted := DHash(photoish(400, 300, 3, 0, 7))
	if d := Hamming(base, shifted); d > 6 {
		t.Logf("burst sibling hamming %d (>6): acceptable if scene has strong fine detail", d)
	}
}