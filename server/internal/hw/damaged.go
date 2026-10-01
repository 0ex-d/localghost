package hw

import (
	"os"
	"path/filepath"
)

// DAMAGED PHOTOS, from the outside: framed moves a photo nothing on the box can read out of the
// archive into <mount>/frames/damaged (internal/framed/damaged.go). secd counts them for Box Status
// and tells the phone it has them, so a damaged file is not sent again.

// DamagedDirOf is the directory under a mounted volume.
func DamagedDirOf(mount string) string { return filepath.Join(mount, "frames", "damaged") }

// DamagedCount is how many photos sit there (list.txt aside).
func DamagedCount(dir string) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() && e.Name() != "list.txt" {
			n++
		}
	}
	return n
}

// DamagedHashes is the hashes of the photos there (the names end "_<hash>.<ext>").
func DamagedHashes(dir string) map[string]bool {
	out := map[string]bool{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range ents {
		name := e.Name()
		ext := filepath.Ext(name)
		base := name[:len(name)-len(ext)]
		if i := lastUnderscore(base); i >= 0 && len(base)-i-1 == 32 {
			out[base[i+1:]] = true
		}
	}
	return out
}

func lastUnderscore(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '_' {
			return i
		}
	}
	return -1
}
