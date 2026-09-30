package search

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LocalGhostDao/localghost/server/internal/imgfit"
)

// A picture past the box's limits is never queued for a caption, and its pHash decode is refused
// from the header alone.
func TestTooLargeToCaption(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.png")
	var buf bytes.Buffer
	png.Encode(&buf, image.NewGray(image.Rect(0, 0, 10, 10)))
	os.WriteFile(small, buf.Bytes(), 0o600)
	if tooLargeToCaption(small) {
		t.Fatal("a 10x10 PNG called too large")
	}
	if tooLargeToCaption(filepath.Join(dir, "missing")) {
		t.Fatal("a missing file called too large (it should be queued and fail visibly)")
	}

	// a small file whose header claims 40000 x 40000
	b := append([]byte(nil), buf.Bytes()...)
	for i, v := range []byte{0, 0, 0x9C, 0x40, 0, 0, 0x9C, 0x40} {
		b[16+i] = v
	}
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29]))
	bomb := filepath.Join(dir, "bomb.png")
	os.WriteFile(bomb, b, 0o600)
	if !tooLargeToCaption(bomb) {
		t.Fatal("a PNG claiming 40000x40000 would be queued")
	}
	if _, err := decodeFile(bomb); err == nil || !strings.Contains(err.Error(), "too large to decode") {
		t.Fatalf("the pHash decode took it: %v", err)
	}

	// a file past the size limit
	big := filepath.Join(dir, "big.raw")
	f, _ := os.Create(big)
	f.Truncate(imgfit.MaxBytes + 1)
	f.Close()
	if !tooLargeToCaption(big) {
		t.Fatal("a file past the size limit would be queued")
	}
}
