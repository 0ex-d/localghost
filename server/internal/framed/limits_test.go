package framed

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// A picture whose header claims more pixels than the box decodes gets no preview, and is never
// decoded (a few KB claiming 40000 x 40000 would be 6.4 GB of pixels).
func TestNoPreviewPastTheLimit(t *testing.T) {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewGray(image.Rect(0, 0, 10, 10)))
	b := buf.Bytes()
	binary.BigEndian.PutUint32(b[16:20], 40000)
	binary.BigEndian.PutUint32(b[20:24], 40000)
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29]))
	p := &Pipeline{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if prev, thumb, why := p.makePreviews(b, "", "abc", 1); prev != "" || thumb != "" || why != "" {
		t.Fatalf("a preview for a decode bomb, or it was called damaged: %q %q %q", prev, thumb, why)
	}
}

// A JPEG whose scan is garbage is called damaged, with Go's words for it, and no preview is made.
func TestDamagedJPEGIsUnreadable(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	sos := bytes.Index(b, []byte{0xFF, 0xDA})
	if sos < 0 {
		t.Fatal("no scan")
	}
	for i := sos + 20; i < len(b)-10; i += 2 {
		b[i], b[i+1] = 0xFF, 0x01 // markers where the entropy-coded data was
	}
	p := &Pipeline{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	prev, thumb, why := p.makePreviews(b, "", "abc", 1)
	if prev != "" || thumb != "" || !strings.HasPrefix(why, "damaged: ") || !strings.Contains(why, "ffmpeg cannot read it either") {
		t.Fatalf("damaged jpeg: %q %q %q", prev, thumb, why)
	}
	if FFmpegWhy(errors.New("exit status 69")) != "exit status 69" {
		t.Fatal("ffmpeg why")
	}
}
