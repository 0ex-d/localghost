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

// A photo nothing can read is moved to frames/damaged at the first stock-take, named by when it was
// taken, with its reason in list.txt; every row the box kept for it goes; the next stock-take has
// nothing to do; the phone is told the box has it.
func TestDamagedPhotoSetAside(t *testing.T) {
	db := fresh(t)
	root := t.TempDir()
	dirs := framed.Dirs{Incoming: filepath.Join(root, "in"), Archive: filepath.Join(root, "archive"), Preview: filepath.Join(root, "preview"),
		Thumb: filepath.Join(root, "thumb"), Paths: filepath.Join(root, "paths"), Damaged: framed.DamagedDir(root)}
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

	_ = db.Exec("INSERT INTO frame_tags (hash, tag, created_at) VALUES ($1, 'beach', 1)", hash)
	_ = db.Exec("INSERT INTO journal_entries (source, ref, ts, title, body, created_at) VALUES ('ghost.framed', $1, 1, 't', 'b', 1)", hash)

	r := p.Converge()
	if r.SetAside != 1 || r.Photos != 0 || r.NoPreview != 0 || r.NoDescription != 0 || asked != 0 {
		t.Fatalf("first pass: %+v asked=%d", r, asked)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the damaged original is still in the archive")
	}
	moved := filepath.Join(dirs.Damaged, "2026-09-30_0000_"+hash+".jpg")
	if fi, err := os.Stat(moved); err != nil || fi.Size() != int64(len(b)) {
		t.Fatalf("not in frames/damaged: %v", err)
	}
	if list, err := os.ReadFile(filepath.Join(dirs.Damaged, "list.txt")); err != nil || !bytes.Contains(list, []byte(hash)) || !bytes.Contains(list, []byte("damaged: ")) {
		t.Fatalf("list.txt: %q %v", list, err)
	}
	for _, q := range []string{"SELECT count(*) FROM frames WHERE hash = $1", "SELECT count(*) FROM frame_tags WHERE hash = $1",
		"SELECT count(*) FROM journal_entries WHERE ref = $1"} {
		if rows, err := db.Query(q, hash); err != nil || *rows.Vals[0][0] != "0" {
			t.Fatalf("%s: a row stayed", q)
		}
	}
	r = p.Converge()
	if r.SetAside != 0 || r.Rederived != 0 || r.Photos != 0 {
		t.Fatalf("second pass: %+v", r)
	}
	if hw.DamagedCount(dirs.Damaged) != 1 || !hw.DamagedHashes(dirs.Damaged)[hash] {
		t.Fatal("the count and the phone's answer")
	}
}
