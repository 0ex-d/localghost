package hw

import (
	"os"
	"path/filepath"
	"testing"
)

// A repeat unlock of a database this secd already converged skips the schema pass; a restarted
// Postgres (new pid or start time) or a new secd process (empty memory) runs it again.
func TestAlreadyConvergedFollowsTheInstance(t *testing.T) {
	mnt := t.TempDir()
	d := NewDataStore(func(int) string { return mnt }, "")
	pg := filepath.Join(mnt, "postgres")
	_ = os.MkdirAll(pg, 0o700)
	pid := func(body string) { _ = os.WriteFile(filepath.Join(pg, "postmaster.pid"), []byte(body), 0o600) }

	if d.alreadyConverged(0) {
		t.Fatal("no pid file, nothing converged: must run")
	}
	pid("2934013\n/var/lib/ghost/mnt/slot0/postgres\n1790975156\n6000\n/var/lib/ghost/mnt/slot0/postgres\n127.0.0.1\n")
	if d.alreadyConverged(0) {
		t.Fatal("a new secd process has converged nothing yet")
	}
	d.converged = map[int]string{0: d.pgInstance(0)}
	if got := d.pgInstance(0); got != "2934013@1790975156" {
		t.Fatalf("instance %q", got)
	}
	if !d.alreadyConverged(0) {
		t.Fatal("same instance, converged by this process: skip")
	}
	if d.alreadyConverged(1) {
		t.Fatal("another slot is not converged")
	}
	pid("2934013\n/var/lib/ghost/mnt/slot0/postgres\n1790975999\n6000\n") // same pid, restarted
	if d.alreadyConverged(0) {
		t.Fatal("a restarted Postgres must be converged again")
	}
	pid("garbage")
	if d.alreadyConverged(0) {
		t.Fatal("an unreadable pid file must not count as converged")
	}
}
