package watchd

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// testLogger is a discard slog logger for tests , the supervisor needs a *slog.Logger but the tests
// assert on process state, not log output.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// writeSleeper creates a tiny executable script that ignores its args and sleeps, standing in for a
// ghost.*d daemon that would otherwise outlive teardown. watchd execs BinPath with --health-port, so
// the script must tolerate extra args (it does: it ignores them).
func writeSleeper(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	script := "#!/bin/sh\nexec sleep 300\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTeardownKillsEveryProcess is the anti-wedge property: after TeardownAll, no supervised process
// is still alive. A lingering daemon holding the encrypted mount open is exactly what blocks unmount,
// so this guards the whole lock path. Real child processes are spawned so "dead" means truly reaped.
func TestTeardownKillsEveryProcess(t *testing.T) {
	dir := t.TempDir()
	jlog := testLogger()
	sup := New(dir, "", jlog)
	for _, name := range []string{"ghost.a", "ghost.b", "ghost.c"} {
		bin := writeSleeper(t, dir, name)
		sup.Register(Service{Name: name, Critical: name == "ghost.a", HealthPort: 0, BinPath: bin})
	}

	if err := sup.StartAll(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// capture the pids before teardown
	sup.mu.Lock()
	var pids []int
	for _, name := range sup.order {
		if p := sup.services[name].proc; p != nil {
			pids = append(pids, p.Pid)
		}
	}
	sup.mu.Unlock()
	if len(pids) != 3 {
		t.Fatalf("expected 3 live procs, got %d", len(pids))
	}

	if err := sup.TeardownAll(); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	if !sup.AllProcessesDead() {
		t.Fatal("supervisor still tracks a live process after teardown")
	}
	// OS-level confirmation: signal 0 to each pid must fail (process gone). Note: the script's `exec
	// sleep` replaces the shell, so the tracked pid IS the sleep , killing it is killing the daemon.
	for _, pid := range pids {
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("process %d survived teardown , this is the unmount wedge", pid)
		}
	}
}

// TestCriticalStartFailurePropagates: a critical service whose binary does not exist makes StartAll
// return an error (secd surfaces it, box serves degraded); a non-critical one does not.
func TestCriticalStartFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	sup := New(dir, "", testLogger())
	sup.Register(Service{Name: "crit", Critical: true, BinPath: filepath.Join(dir, "does-not-exist")})
	if err := sup.StartAll(context.Background()); err == nil {
		t.Fatal("critical start failure must propagate from StartAll")
	}
	_ = sup.TeardownAll()
}

func TestNonCriticalStartFailureDoesNotPropagate(t *testing.T) {
	dir := t.TempDir()
	sup := New(dir, "", testLogger())
	sup.Register(Service{Name: "opt", Critical: false, BinPath: filepath.Join(dir, "does-not-exist")})
	if err := sup.StartAll(context.Background()); err != nil {
		t.Fatalf("non-critical start failure must NOT propagate, got %v", err)
	}
	_ = sup.TeardownAll()
}
