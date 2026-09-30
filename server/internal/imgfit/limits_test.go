package imgfit

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestLimits(t *testing.T) {
	if err := CheckPixels(8064, 6048); err != nil {
		t.Fatalf("a 48 MP phone photo refused: %v", err)
	}
	var tl ErrTooLarge
	if err := CheckPixels(30000, 30000); !errors.As(err, &tl) {
		t.Fatalf("a decode bomb passed: %v", err)
	}
	if err := CheckPixels(0, 10); err == nil {
		t.Fatal("a zero size passed")
	}
	// the header alone decides: a small file that claims a huge picture
	var buf bytes.Buffer
	png.Encode(&buf, image.NewGray(image.Rect(0, 0, 10, 10)))
	b := buf.Bytes()
	// IHDR width/height live at bytes 16..24; claim 40000 x 40000
	for i, v := range []byte{0, 0, 0x9C, 0x40, 0, 0, 0x9C, 0x40} {
		b[16+i] = v
	}
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29])) // the chunk's CRC, as a real file has
	if err := CheckHeader(bytes.NewReader(b)); !errors.As(err, &tl) {
		t.Fatalf("a PNG claiming 40000x40000 passed: %v", err)
	}
	p := filepath.Join(t.TempDir(), "big")
	f, _ := os.Create(p)
	f.Truncate(MaxBytes + 1)
	f.Close()
	if err := CheckFile(p); !errors.As(err, &tl) {
		t.Fatalf("a file past the size limit passed: %v", err)
	}
}
