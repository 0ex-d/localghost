package framed

// DAMAGED PHOTOS ARE SET ASIDE. A photo whose original neither Go's decoder nor ffmpeg can read
// (a damaged file: a block of zeros where the picture was, a header that is not one) has nothing
// for the box to show, describe or place. It is moved out of the archive into frames/damaged,
// named by when it was taken so it can be looked for on the phone ("2026-09-30_1432_<hash>.jpg"),
// with a line in damaged/list.txt saying why, and every row the box kept for it goes (the frame,
// its tags, its journal line, searchd's original and its queued jobs). Deleting them is the
// owner's: sudo ./tools/ns.sh ls /var/lib/ghost/mnt/slot0/frames/damaged

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// errSetAside says a photo was moved to frames/damaged and has no frame any more.
var errSetAside = errors.New("damaged: set aside in frames/damaged")

// DamagedDir is where damaged photos are moved, under a volume's frames directory.
func DamagedDir(framesRoot string) string { return filepath.Join(framesRoot, "damaged") }

// setAside moves a damaged original out of the archive and forgets it.
func (p *Pipeline) setAside(hash, archivePath string, takenAt int64, why string) error {
	if err := os.MkdirAll(p.dirs.Damaged, 0o750); err != nil {
		return err
	}
	name := time.Unix(takenAt, 0).UTC().Format("2006-01-02_1504") + "_" + hash + strings.ToLower(filepath.Ext(archivePath))
	dest := filepath.Join(p.dirs.Damaged, name)
	if err := os.Rename(archivePath, dest); err != nil && !os.IsNotExist(err) {
		return err
	}
	if f, err := os.OpenFile(filepath.Join(p.dirs.Damaged, "list.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640); err == nil {
		_, _ = fmt.Fprintf(f, "%s\t%s\n", name, strings.ReplaceAll(why, "\n", " "))
		_ = f.Close()
	}
	for _, d := range []string{p.dirs.Preview, p.dirs.Thumb} {
		for _, ext := range []string{".jpg", ".webp"} {
			_ = os.Remove(filepath.Join(d, hash+ext))
		}
	}
	if err := p.store.ForgetFrame(hash); err != nil {
		return err
	}
	p.work.Add("damaged set aside", 1)
	p.log.Warn("damaged photo set aside", "fn", "setAside", "hash", hash, "file", name, "why", why)
	return nil
}
