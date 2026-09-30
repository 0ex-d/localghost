package watchd

import (
	"os"
	"path/filepath"
	"testing"
)

// The cohort's TMPDIR is on the volume, private, and starts empty.
func TestTmpDirOnTheVolume(t *testing.T) {
	mount := t.TempDir()
	stale := filepath.Join(mount, "tmp", "lg-redecode-1.jpg")
	_ = os.MkdirAll(filepath.Dir(stale), 0o755)
	_ = os.WriteFile(stale, []byte("left by a crash"), 0o600)
	s := New(mount, "", nil)
	d := s.tmpDir(nil)
	if d != filepath.Join(mount, "tmp") {
		t.Fatalf("tmp dir %q", d)
	}
	st, err := os.Stat(d)
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v %v", st.Mode(), err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("a crash's leftovers survived")
	}
	// the second daemon gets the same dir and does not empty it under the first
	_ = os.WriteFile(filepath.Join(d, "in-use"), nil, 0o600)
	if s.tmpDir(nil) != d {
		t.Fatal("a different dir")
	}
	if _, err := os.Stat(filepath.Join(d, "in-use")); err != nil {
		t.Fatal("emptied while in use")
	}
}
