package zstd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The upstream testdata: each file named after the first eight hex digits of its content's SHA-256.
func TestTestdataFiles(t *testing.T) {
	files, err := filepath.Glob("testdata/*.zst")
	if err != nil || len(files) == 0 {
		t.Fatal("no testdata", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(NewReader(bytes.NewReader(b)))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		sum := sha256.Sum256(out)
		if want := strings.SplitN(filepath.Base(f), ".", 2)[0]; hex.EncodeToString(sum[:4]) != want {
			t.Errorf("%s: %x", f, sum[:4])
		}
	}
}
