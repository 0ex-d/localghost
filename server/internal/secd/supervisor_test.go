package secd

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestSupervisorTeardownKillsEveryProcess is the anti-wedge property: after Teardown, no supervised
// process is still alive. A lingering process (e.g. Postgres) holding the encrypted mount open is
// exactly what blocks unmount, so this is the test that guards the whole lock path. It spawns real
// child processes (sleep) so "dead" means genuinely reaped, not just marked.
func TestSupervisorTeardownKillsEveryProcess(t *testing.T) {
	sup := NewSupervisor()

	var procs []*os.Process
	for _, name := range []string{"svc-a", "svc-b", "svc-c"} {
		name := name
		sup.Register(SupervisedService{
			Name:       name,
			Critical:   name == "svc-a",
			HealthPort: 0, // not health-polled in this test; we assert process death, not health
			Start: func() (*os.Process, error) {
				// a long sleep stands in for a daemon that would otherwise outlive teardown
				cmd := exec.Command("sleep", "300")
				cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				if err := cmd.Start(); err != nil {
					return nil, err
				}
				procs = append(procs, cmd.Process)
				return cmd.Process, nil
			},
			Stop: func() error { return nil },
		})
	}

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	// let them come up
	time.Sleep(200 * time.Millisecond)

	if err := sup.Teardown(); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	if !sup.AllProcessesDead() {
		t.Fatal("supervisor still tracks a live process after teardown")
	}
	// Independently confirm at the OS level: signal 0 to each PID must fail (process gone).
	for _, p := range procs {
		if err := p.Signal(syscall.Signal(0)); err == nil {
			t.Fatalf("process %d survived teardown , this is the unmount wedge", p.Pid)
		}
	}
}

// TestSupervisorCriticalStartFailurePropagates: a critical service that cannot start makes Start
// return an error (so the unlock flow can serve degraded), while a non-critical failure does not.
func TestSupervisorCriticalStartFailurePropagates(t *testing.T) {
	sup := NewSupervisor()
	sup.Register(SupervisedService{
		Name: "crit", Critical: true,
		Start: func() (*os.Process, error) { return nil, errReject },
		Stop:  func() error { return nil },
	})
	if err := sup.Start(context.Background()); err == nil {
		t.Fatal("critical start failure must propagate from Start")
	}
	_ = sup.Teardown()
}

func TestSupervisorNonCriticalStartFailureDoesNotPropagate(t *testing.T) {
	sup := NewSupervisor()
	sup.Register(SupervisedService{
		Name: "opt", Critical: false,
		Start: func() (*os.Process, error) { return nil, errReject },
		Stop:  func() error { return nil },
	})
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("non-critical start failure must NOT propagate, got %v", err)
	}
	_ = sup.Teardown()
}
