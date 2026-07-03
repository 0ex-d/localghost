package secd

// The supervisor: ghost.secd owns the lifecycle of every process that lives on the encrypted volume
// (the ghost.*d daemons). It is NOT systemd , systemd cannot see inside a volume that only exists
// after PIN entry. secd starts the daemons after mount, polls each one's loopback /health every 5s,
// restarts on failure with capped backoff, and , the property that matters most on this box , stops
// every one of them and CONFIRMS the process is dead before the volume is unmounted. A daemon left
// holding the mount open is the unmount wedge, the same class of failure that cost us the TPM.
//
// Tiers (the `critical` flag on each service):
//   - critical (shadowd, and , managed separately by DataStore , Postgres + Redis): if it will not
//     stay up, capability endpoints that need it return honest errors. The box does NOT lock, does
//     NOT unmount, does NOT go dark to the authenticated owner. It stays mounted and serves what it
//     can, logging why the rest is unavailable.
//   - non-critical: restart with backoff; if it stays dead, log and carry on. Its capability errors,
//     the rest of the box works.
//
// Nothing here auto-locks. Lock is always operator-driven; this only supervises while unlocked.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/ghosthealth"
)

// serviceState is the per-service state machine. The transitions exist to stop a mid-restart
// no-response from triggering ANOTHER restart , that thundering loop, on Postgres, forks processes
// over one data dir and wedges the mount. A service in Restarting/Backoff is not polled for restart.
type serviceState int

const (
	stateDown       serviceState = iota // not started yet
	stateUp                             // running, last poll ok/degraded
	stateRestarting                     // start issued, within grace period, not yet polled
	stateBackoff                        // failed; waiting out the backoff before next start
)

func (s serviceState) String() string {
	switch s {
	case stateUp:
		return "up"
	case stateRestarting:
		return "restarting"
	case stateBackoff:
		return "backoff"
	default:
		return "down"
	}
}

// SupervisedService describes one managed process. Start/Stop are indirected so a service can be a
// real binary (exec) OR delegate to something else (Postgres/Redis go through DataStore, so there is
// exactly one lifecycle for them, never a parallel one).
type SupervisedService struct {
	Name       string
	Critical   bool
	HealthPort int // loopback port its /health is on; 0 => not health-polled (delegated services)
	// Start launches the process and returns it so the supervisor can signal + reap it. For delegated
	// services (Postgres/Redis) Start returns (nil, err-or-nil) and Stop does the real teardown.
	Start func() (*os.Process, error)
	Stop  func() error
}

// serviceRuntime is the live state the supervisor keeps per service.
type serviceRuntime struct {
	svc        SupervisedService
	state      serviceState
	proc       *os.Process
	restarts   int
	lastErr    string
	lastCode   ghosthealth.Code
	backoffTil time.Time
}

// Supervisor manages a set of services under one lock.
type Supervisor struct {
	mu       sync.Mutex
	services map[string]*serviceRuntime
	order    []string // start order, deterministic
	pollEach time.Duration
	client   *http.Client
	stopPoll context.CancelFunc
	wg       sync.WaitGroup
	log      *log.Logger
}

func NewSupervisor() *Supervisor {
	return &Supervisor{
		services: map[string]*serviceRuntime{},
		pollEach: 5 * time.Second,
		client:   &http.Client{Timeout: 2 * time.Second},
		log:      log.New(os.Stderr, "supervisor ", log.LstdFlags),
	}
}

// Register adds a service (before Start). Order of registration is start order.
func (s *Supervisor) Register(svc SupervisedService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.services[svc.Name]; ok {
		return
	}
	s.services[svc.Name] = &serviceRuntime{svc: svc, state: stateDown}
	s.order = append(s.order, svc.Name)
}

// Start launches every registered service in order, then begins the poll loop. A CRITICAL service
// that fails to start is returned as an error (the caller , the unlock flow , surfaces it, and the
// box serves with that capability erroring); a non-critical start failure is logged and supervised
// for restart. This never blocks the box from being "mounted and serving".
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	var criticalErr error
	for _, name := range s.order {
		rt := s.services[name]
		if err := s.startOne(rt); err != nil {
			rt.lastErr = err.Error()
			if rt.svc.Critical {
				s.log.Printf("CRITICAL service %s failed to start: %v", name, err)
				if criticalErr == nil {
					criticalErr = fmt.Errorf("critical service %s: %w", name, err)
				}
			} else {
				s.log.Printf("service %s failed to start (non-critical): %v", name, err)
			}
		}
	}
	pctx, cancel := context.WithCancel(ctx)
	s.stopPoll = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go s.pollLoop(pctx)
	return criticalErr
}

// startOne launches a single service and moves it to Restarting (grace period before first poll).
func (s *Supervisor) startOne(rt *serviceRuntime) error {
	proc, err := rt.svc.Start()
	if err != nil {
		rt.state = stateBackoff
		return err
	}
	rt.proc = proc
	rt.state = stateRestarting
	rt.backoffTil = time.Now().Add(3 * time.Second) // grace before we trust a poll
	return nil
}

func (s *Supervisor) pollLoop(ctx context.Context) {
	defer s.wg.Done()
	t := time.NewTicker(s.pollEach)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.pollOnce()
		}
	}
}

// pollOnce checks each service. A service in Restarting whose grace has elapsed is promoted to Up on
// a good poll; a service in Backoff whose timer elapsed is restarted; a service Up that fails a poll
// (bad code or no response) is restarted with growing backoff.
func (s *Supervisor) pollOnce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, name := range s.order {
		rt := s.services[name]
		switch rt.state {
		case stateRestarting:
			if now.Before(rt.backoffTil) {
				continue // still in grace
			}
			rt.state = stateUp // grace elapsed; treat as up, next poll validates
			fallthrough
		case stateUp:
			code, err := s.probe(rt)
			if err != nil || code == ghosthealth.Failed {
				s.markFailedAndRestart(rt, code, err)
			} else {
				rt.lastCode = code
				rt.lastErr = ""
			}
		case stateBackoff:
			if now.After(rt.backoffTil) {
				s.log.Printf("service %s: restarting after backoff (attempt %d)", rt.svc.Name, rt.restarts+1)
				if err := s.startOne(rt); err != nil {
					s.scheduleBackoff(rt, err.Error())
				}
			}
		}
	}
}

// probe reads the service's /health. Delegated services (HealthPort 0) are considered up if their
// Stop-target is alive; here we treat HealthPort 0 as "not polled" and always ok (DataStore owns
// pg/redis liveness). A real /health poll reads only Code.
func (s *Supervisor) probe(rt *serviceRuntime) (ghosthealth.Code, error) {
	if rt.svc.HealthPort == 0 {
		return ghosthealth.OK, nil
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", rt.svc.HealthPort)
	resp, err := s.client.Get(url)
	if err != nil {
		return ghosthealth.Failed, err
	}
	defer resp.Body.Close()
	var h ghosthealth.Health
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return ghosthealth.Failed, err
	}
	// Identity check: a port collision (something else answering) must not read as healthy.
	if h.Name != "" && h.Name != rt.svc.Name {
		return ghosthealth.Failed, fmt.Errorf("port %d answered as %q, expected %q",
			rt.svc.HealthPort, h.Name, rt.svc.Name)
	}
	return h.Code, nil
}

func (s *Supervisor) markFailedAndRestart(rt *serviceRuntime, code ghosthealth.Code, err error) {
	detail := "health code failed"
	if err != nil {
		detail = err.Error()
	}
	s.log.Printf("service %s failed (%s); scheduling restart", rt.svc.Name, detail)
	// kill the old process if still around, so a hung (not dead) process is not left behind
	s.killProc(rt)
	s.scheduleBackoff(rt, detail)
}

// scheduleBackoff sets an exponential, capped backoff. Cap at 60s so a permanently-dead non-critical
// service is retried once a minute, not in a tight loop.
func (s *Supervisor) scheduleBackoff(rt *serviceRuntime, detail string) {
	rt.restarts++
	rt.lastErr = detail
	rt.state = stateBackoff
	backoff := time.Duration(1<<min(rt.restarts, 6)) * time.Second // 2,4,...,64 -> capped
	if backoff > 60*time.Second {
		backoff = 60 * time.Second
	}
	rt.backoffTil = time.Now().Add(backoff)
}

// killProc signals TERM then, after a grace, KILL, and reaps. Used on failure and on teardown.
func (s *Supervisor) killProc(rt *serviceRuntime) {
	if rt.proc == nil {
		return
	}
	_ = rt.proc.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _, _ = rt.proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = rt.proc.Kill()
		<-done
	}
	rt.proc = nil
}

// Teardown stops the poll loop, then stops EVERY service in reverse start order and CONFIRMS each
// process is gone. Returns only after every supervised process is dead , this is the property the
// unmount depends on. The caller unmounts AFTER this returns nil. Delegated services (pg/redis) are
// stopped via their Stop func (DataStore), which itself confirms shutdown before returning.
func (s *Supervisor) Teardown() error {
	s.mu.Lock()
	if s.stopPoll != nil {
		s.stopPoll() // stop polling so nothing restarts mid-teardown
	}
	s.mu.Unlock()
	s.wg.Wait() // poll loop fully stopped

	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	// reverse order: dependents down before their dependencies
	for i := len(s.order) - 1; i >= 0; i-- {
		rt := s.services[s.order[i]]
		if rt.svc.Stop != nil {
			if err := rt.svc.Stop(); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("stop %s: %w", rt.svc.Name, err)
			}
		}
		s.killProc(rt) // confirm the process is dead regardless of Stop's mechanism
		rt.state = stateDown
	}
	return firstErr
}

// Status returns a snapshot for /v1/status: what secd knows from supervision (state, restarts, last
// error). watchd's Redis metrics are merged on top by the status handler.
func (s *Supervisor) Status() []ServiceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ServiceStatus, 0, len(s.order))
	for _, name := range s.order {
		rt := s.services[name]
		out = append(out, ServiceStatus{
			Name:     rt.svc.Name,
			Critical: rt.svc.Critical,
			State:    rt.state.String(),
			Restarts: rt.restarts,
			LastErr:  rt.lastErr,
			Code:     uint8(rt.lastCode),
		})
	}
	return out
}

// ServiceStatus is secd's supervisor view of one service (merged with watchd metrics in /v1/status).
type ServiceStatus struct {
	Name     string `json:"name"`
	Critical bool   `json:"critical"`
	State    string `json:"state"`
	Restarts int    `json:"restarts"`
	LastErr  string `json:"lastErr,omitempty"`
	Code     uint8  `json:"code"`
}

// AllProcessesDead reports whether no supervised process is still running. Used by the unmount-safety
// test to assert the anti-wedge property after Teardown.
func (s *Supervisor) AllProcessesDead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range s.order {
		if s.services[name].proc != nil {
			return false
		}
	}
	return true
}

// spawnDaemon is the standard Start for a real ghost.*d binary: exec it with its health port in the
// env, in its own process group so we can signal the whole group on teardown.
func spawnDaemon(bin string, healthPort int, extraEnv ...string) func() (*os.Process, error) {
	return func() (*os.Process, error) {
		cmd := exec.Command(bin, "--health-port", fmt.Sprint(healthPort))
		cmd.Env = append(os.Environ(), append([]string{
			fmt.Sprintf("GHOST_HEALTH_PORT=%d", healthPort),
		}, extraEnv...)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd.Process, nil
	}
}
