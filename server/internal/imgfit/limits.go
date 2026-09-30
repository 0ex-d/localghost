package imgfit

import (
	"fmt"
	"image"
	"io"
	"os"
)

// THE SIZE LIMITS: what the box will decode at all. Decoding costs memory by pixel, not by file:
// a PNG of a few MB can claim 30000 x 30000 pixels (3.6 GB once decoded), and a 100 MB file (a RAW,
// a scan, a panorama) is read whole before anything else happens. So nothing is decoded before its
// file size and the pixel count its header claims are checked. A photo past either is archived
// untouched and searchable by its date and place; it gets no preview and no caption, and the log
// says why.
const (
	// MaxBytes: a file larger than this is never read whole to be decoded. A 48 MP phone JPEG is
	// 15 to 25 MB; a RAW is 25 to 100.
	MaxBytes = 64 << 20
	// MaxPixels: a header claiming more than this is not decoded (60 MP: every phone camera
	// fits, at 4 bytes a pixel that is 240 MB of memory at worst).
	MaxPixels = 60_000_000
)

// ErrTooLarge says an image is past the limits; the caller records it and moves on.
type ErrTooLarge struct{ What string }

func (e ErrTooLarge) Error() string { return "too large to decode: " + e.What }

// CheckFile: the file is small enough to read whole.
func CheckFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Size() > MaxBytes {
		return ErrTooLarge{fmt.Sprintf("%d MB (the limit is %d MB)", fi.Size()>>20, MaxBytes>>20)}
	}
	return nil
}

// CheckPixels: the header's size is one to decode.
func CheckPixels(w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("%dx%d is not an image size", w, h)
	}
	if int64(w)*int64(h) > MaxPixels {
		return ErrTooLarge{fmt.Sprintf("%dx%d pixels (the limit is %d MP)", w, h, MaxPixels/1_000_000)}
	}
	return nil
}

// CheckHeader reads only the header from r and checks its size.
func CheckHeader(r io.Reader) error {
	cfg, _, err := image.DecodeConfig(r)
	if err != nil {
		return err
	}
	return CheckPixels(cfg.Width, cfg.Height)
}
