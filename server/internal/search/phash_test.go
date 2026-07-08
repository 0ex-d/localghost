package search

import (
	"image"
	"image/color"
	"testing"
)

func gradientImg(w, h int, shift uint8) image.Image {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(x*255/w) + shift})
		}
	}
	return img
}

func TestDHashStableUnderResizeAndBrightness(t *testing.T) {
	a := DHash(gradientImg(400, 300, 0))
	b := DHash(gradientImg(200, 150, 0))  // same picture, half size
	c := DHash(gradientImg(400, 300, 10)) // same picture, brighter
	if d := Hamming(a, b); d > 6 {
		t.Fatalf("resize should be a near-dup, hamming %d", d)
	}
	if d := Hamming(a, c); d > 6 {
		t.Fatalf("brightness shift should be a near-dup, hamming %d", d)
	}
	if !NearDup(a, b) {
		t.Fatal("NearDup must accept hamming <= 6")
	}
}

func TestDHashSeparatesDifferentImages(t *testing.T) {
	grad := DHash(gradientImg(300, 300, 0))
	// A vertical gradient reads very differently under a horizontal-gradient hash.
	img := image.NewGray(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8(y * 255 / 300)})
		}
	}
	if d := Hamming(grad, DHash(img)); d <= 6 {
		t.Fatalf("distinct images must not collide, hamming %d", d)
	}
}
