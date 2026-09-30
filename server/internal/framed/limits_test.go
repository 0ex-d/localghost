package framed

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"log/slog"
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
	if prev, thumb := p.makePreviews(b, "", "abc", 1); prev != "" || thumb != "" {
		t.Fatalf("a preview for a decode bomb: %q %q", prev, thumb)
	}
}
