package pgtest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/LocalGhostDao/localghost/server/internal/framed"
	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// A photo nothing can read is marked at the first stock-take, counted apart, not read again or
// sent for a caption at the next, and listed with what its file says; the pipeline's stages leave
// it out of their totals.
func TestUnreadablePhotoSetApart(t *testing.T) {
	db := fresh(t)
	root := t.TempDir()
	dirs := framed.Dirs{Incoming: filepath.Join(root, "in"), Archive: filepath.Join(root, "archive"), Preview: filepath.Join(root, "preview"),
		Thumb: filepath.Join(root, "thumb"), Paths: filepath.Join(root, "paths")}
	for _, d := range []string{dirs.Incoming, dirs.Archive, dirs.Preview, dirs.Thumb, dirs.Paths} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// a JPEG whose scan is garbage, and a block of zeros where the rest of the picture was
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 13)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	sos := bytes.Index(b, []byte{0xFF, 0xDA})
	for i := sos + 20; i < len(b)-10; i += 2 {
		b[i], b[i+1] = 0xFF, 0x01
	}
	b = append(b[:len(b)-2], make([]byte, 4096)...)
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:16])
	day := filepath.Join(dirs.Archive, "2026", "09", "30")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(day, hash+".jpg")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO frames (hash, taken_at, archive_path, bytes, kind, mime, taken_src, pipe_ver, display_name)
		VALUES ($1, 1790726400, $2, $3, 'photo', 'image/jpeg', 'exif', $4, 'Wednesday 30 September 2026')`, hash, path, len(b), framed.PipelineVersion); err != nil {
		t.Fatal(err)
	}
	p := framed.NewPipeline(dirs, framed.NewStoreDB(db), slog.New(slog.NewTextHandler(io.Discard, nil)))
	// this machine's ffmpeg reads more than the box's did with the real files: none here, so the
	// garbage is as unreadable as theirs
	p.SetFFmpeg(filepath.Join(root, "no-ffmpeg"), "")
	asked := 0
	p.OnArchived(func(string, string, int64, bool) { asked++ })

	r := p.Converge()
	if r.Unreadable != 1 || r.NoPreview != 0 || r.NoDescription != 0 || asked != 0 {
		t.Fatalf("first pass: %+v asked=%d", r, asked)
	}
	rows, err := db.Query("SELECT unreadable FROM frames WHERE hash = $1", hash)
	if err != nil || len(rows.Vals) != 1 || rows.Vals[0][0] == nil || len(*rows.Vals[0][0]) < 10 {
		t.Fatalf("not marked: %v %v", rows, err)
	}
	r = p.Converge()
	if r.Unreadable != 1 || r.Rederived != 0 || asked != 0 {
		t.Fatalf("second pass read it again or asked for a caption: %+v asked=%d", r, asked)
	}
	pp, err := hw.PipelineProgressFrom(db)
	if err != nil || pp.Unreadable != 1 || pp.Total != 0 || pp.Photos != 1 {
		t.Fatalf("pipeline: %+v %v", pp, err)
	}
	list, err := p.CheckUnreadable(10)
	if err != nil || len(list) != 1 {
		t.Fatalf("check: %v %v", list, err)
	}
	c := list[0]
	if !c.Intact || c.Starts != "a JPEG's start" || c.ZeroRun < 4096 || c.ZeroTail < 4096 || c.OnDisk != int64(len(b)) {
		t.Fatalf("check: %+v", c)
	}
}
