package watchd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestArchiveCompletedDays: today and yesterday stay PLAIN (the 24-48h uncompressed window); only the
// day-before-yesterday and older get gzipped.
func TestArchiveCompletedDays(t *testing.T) {
	dir := t.TempDir()
	td := today()
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	twoAgo := time.Now().AddDate(0, 0, -2).Format("2006-01-02")

	cur := filepath.Join(dir, "ghost.noted-"+td+".log")
	yd := filepath.Join(dir, "ghost.noted-"+yesterday+".log")
	old := filepath.Join(dir, "ghost.noted-"+twoAgo+".log")
	for _, p := range []string{cur, yd, old} {
		if err := os.WriteFile(p, []byte("x\n"), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	NewRoller(dir).archiveCompletedDays()

	// two-days-ago is archived and its plain file removed
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("day-before-yesterday's plain .log should be removed after archiving")
	}
	if _, err := os.Stat(filepath.Join(dir, "archive", "ghost.noted-"+twoAgo+".log.gz")); err != nil {
		t.Fatalf("day-before-yesterday should be gzipped: %v", err)
	}
	// today and yesterday stay plain, untouched
	if _, err := os.Stat(cur); err != nil {
		t.Fatal("today's log must be left plain")
	}
	if _, err := os.Stat(yd); err != nil {
		t.Fatal("yesterday's log must be left plain (24-48h window)")
	}
	if _, err := os.Stat(filepath.Join(dir, "archive", "ghost.noted-"+yesterday+".log.gz")); err == nil {
		t.Fatal("yesterday must NOT be archived yet")
	}
}

// TestPruneKeepsSevenDays: with more than 7 daily archives for a daemon, only the 7 newest survive.
func TestPruneKeepsSevenDays(t *testing.T) {
	dir := t.TempDir()
	archDir := filepath.Join(dir, "archive")
	if err := os.MkdirAll(archDir, 0o750); err != nil {
		t.Fatal(err)
	}
	// 10 consecutive days of archives, all within the cutoff window is not required , prune keeps the
	// 7 newest by name regardless. Use recent dates so the date-cutoff branch does not also fire.
	for i := 0; i < 10; i++ {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		p := filepath.Join(archDir, "ghost.noted-"+day+".log.gz")
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	NewRoller(dir).prune()

	entries, _ := os.ReadDir(archDir)
	if len(entries) != retentionDays {
		t.Fatalf("expected %d archives after prune, got %d", retentionDays, len(entries))
	}
}

// TestPruneDropsBeyondCutoff: an archive older than the retention window is deleted even if there are
// fewer than 7 for that daemon.
func TestPruneDropsBeyondCutoff(t *testing.T) {
	dir := t.TempDir()
	archDir := filepath.Join(dir, "archive")
	_ = os.MkdirAll(archDir, 0o750)
	oldDay := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	p := filepath.Join(archDir, "ghost.noted-"+oldDay+".log.gz")
	_ = os.WriteFile(p, []byte("x"), 0o640)

	NewRoller(dir).prune()

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("an archive older than the retention window must be pruned")
	}
}
