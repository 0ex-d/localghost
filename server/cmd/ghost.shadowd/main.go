// ghost.shadowd , the ANTI-POSSESSION DAEMON (charter: hard-truths/should-not-possess,
// /dictator-brain, /critic-worth-listening-to). NOT the memory layer , that is ghost.synthd. This
// daemon is a fleet of manipulation-pattern DETECTORS (the 28-entry catalogue: gaslighting, DARVO,
// coercive control, addiction-by-design, engagement-driven loneliness, filter-bubble-of-one,
// sunk-cost amplification, memory laundering, self-narrative calcification, arbiter capture, and
// the rest) plus the COLD-READ ARBITER , a separate model that was never shaped by the user, so it
// can disagree (you cannot prompt your way out of a reward function; you cannot configure your way
// out of one either, hence the scheduled arbiter reset to a published baseline).
//
// The contract, from the charter: NAMING IS THE ACTION. Detectors name the pattern, point at the
// data, and stop. No blocking, no refusing, no lockouts unless the user wrote the rule themselves
// (Ulysses contracts, parental controls). Detectors are individually tunable and mutable for
// bounded windows, never disableable as a fleet. The first tractable detectors are the
// [LOCALGHOST]-origin ones that read data the box already holds: total interaction time and
// session-past-task-completion (addiction by design), share of emotional processing done with the
// ghost vs humans (engagement-driven loneliness), sunk-cost retrieval framing, topic-surface
// narrowing (filter bubble of one). This binary is the honest stub of all that: health + ctl only,
// detectors pending, and the box will not ship v1 without it running.
//
// Runs only while the account is UNLOCKED (data lives on the encrypted volume). Spawned by ghost.watchd
// from <mount>/bin/ghost.shadowd; its stdout/stderr go to <mount>/logs/ghost.shadowd.log.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
	"github.com/LocalGhostDao/localghost/server/internal/ghosthealth"
	"github.com/LocalGhostDao/localghost/server/internal/rotlog"
	"github.com/LocalGhostDao/localghost/server/internal/svcconf"
)

const service = "ghost.shadowd"

func main() {
	port := flag.Int("health-port", envPort("GHOST_HEALTH_PORT"), "loopback health/status port (required)")
	flag.Parse()
	if *port <= 0 {
		log.Fatalf("%s: no health port (set --health-port or GHOST_HEALTH_PORT)", service)
	}

	// Logs go through a self-rotating writer: <GHOST_LOG_DIR>/<service>-YYYY-MM-DD.log, a new file at
	// midnight with no restart (watchd sets GHOST_LOG_DIR when it spawns us). If GHOST_LOG_DIR is
	// unset (run by hand), fall back to stderr so nothing is lost.
	var lg *slog.Logger
	var lvl *slog.LevelVar
	if dir := os.Getenv("GHOST_LOG_DIR"); dir != "" {
		w, err := rotlog.New(dir, service)
		if err != nil {
			log.Fatalf("%s: open log: %v", service, err)
		}
		defer w.Close()
		lg, lvl = rotlog.Logger(w)
	} else {
		lvl = new(slog.LevelVar)
		lvl.Set(rotlog.LevelFromEnv())
		lg = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	srv := ghosthealth.NewServer(service, ghosthealth.OKReporter{Service: service})
	go func() {
		if err := srv.Serve(*port); err != nil {
			lg.Error("health server stopped", "fn", "main", "err", err)
		}
	}()
	// Control socket: base commands (ping/status/reload/log-level/commands) so ghost-cli and watchd
	// can talk to this daemon. A stub has no service-specific commands yet; real logic adds its own.
	runDir := os.Getenv("GHOST_RUN_DIR")
	if runDir == "" {
		if ld := os.Getenv("GHOST_LOG_DIR"); ld != "" {
			runDir = filepath.Join(filepath.Dir(ld), "run")
		}
	}
	if runDir != "" {
		ctl := ctlsock.NewServer(service, runDir, lg)
		svcconf.BindBase(ctl, service, lvl, func() (svcconf.Base, map[string]string, error) {
			mount := filepath.Dir(runDir)
			base := svcconf.DefaultBase()
			_ = svcconf.Load(svcconf.Path(mount, service), &base)
			svcconf.FillBaseDefaults(&base)
			return base, nil, nil
		})
		defer ctl.Cleanup()
		go func() {
			if err := ctl.Serve(ctx); err != nil {
				lg.Error("control server exited", "fn", "main", "err", err)
			}
		}()
	}

	lg.Info("stub up (charter recorded, detectors pending)", "fn", "main", "healthPort", *port)

	<-ctx.Done()
	lg.Info("shutting down", "fn", "main")
}

func envPort(key string) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}
