// ghost.tallyd , STUB. Binds its loopback health port and reports OK so ghost.watchd can
// manage it (poll, restart, stop-before-unmount) before the real logic exists. The daemon's actual
// job is described in this directory's README; this binary is the honest placeholder , it does
// nothing but stay alive and answer health, so the supervisor and the app's Ghost Status screen work
// end to end today. Replace the body with real logic behind the same ghosthealth.Reporter contract.
//
// Runs only while the account is UNLOCKED (data lives on the encrypted volume). Exits cleanly on
// SIGTERM so the supervisor's stop-and-confirm-dead teardown never leaves it holding the mount.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
	"github.com/LocalGhostDao/localghost/server/internal/ghosthealth"
	"github.com/LocalGhostDao/localghost/server/internal/rotlog"
	"github.com/LocalGhostDao/localghost/server/internal/svcconf"
)

const service = "ghost.tallyd"

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

	// FIRST REAL SLICE: health ingestion. The app reads the phone's Health Connect store (where
	// Samsung Health writes) and posts day-batches; secd drops them as JSON in <mount>/tallyd/
	// inbox; this loop upserts health_metrics and journals each day once , which is how sleep and
	// movement reach synthd's distillation and the check-in's suggestions. Structured data in,
	// time-series + diary out , exactly the charter.
	if runDir != "" {
		go healthLoop(ctx, filepath.Dir(runDir), lg)
		lg.Info("health ingestion up", "fn", "main")
	}

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

// healthLoop polls tallyd's inbox for health day-batches , the same lazy-pg inbox pattern as noted.
func healthLoop(ctx context.Context, mount string, lg *slog.Logger) {
	inbox := filepath.Join(mount, "tallyd", "inbox")
	done := filepath.Join(mount, "tallyd", "done")
	for _, d := range []string{inbox, done} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			lg.Error("inbox dirs", "fn", "healthLoop", "err", err)
			return
		}
	}
	var db *poltergres.ReadWrite
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		entries, err := os.ReadDir(inbox)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if db == nil {
				cfg, cerr := hw.LoadServicesConfig(mount)
				if cerr != nil {
					break
				}
				db = poltergres.NewReadWrite(hw.SocketForMount(mount), cfg.Postgres.Port,
					cfg.Postgres.RWUser, cfg.Postgres.RWPass, cfg.Postgres.Name)
			}
			path := filepath.Join(inbox, e.Name())
			if err := ingestHealth(db, path, lg); err != nil {
				lg.Warn("health ingest failed, will retry next tick", "fn", "healthLoop", "file", e.Name(), "err", err)
				db = nil
				continue
			}
			_ = os.Rename(path, filepath.Join(done, e.Name()))
		}
	}
}

type healthDay struct {
	Day     string             `json:"day"`
	Metrics map[string]float64 `json:"metrics"`
}

type healthSample struct {
	Metric string  `json:"metric"`
	TS     int64   `json:"ts"`
	Value  float64 `json:"value"`
}

func ingestHealth(db *poltergres.ReadWrite, path string, lg *slog.Logger) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var batch struct {
		Days    []healthDay    `json:"days"`
		Samples []healthSample `json:"samples"`
	}
	if err := json.Unmarshal(raw, &batch); err != nil {
		// Malformed is a rename-to-done with a log, not a retry loop.
		lg.Warn("health batch unparseable, skipped", "fn", "ingestHealth", "err", err)
		return nil
	}
	for _, sm := range batch.Samples {
		if sm.TS <= 0 || len(sm.Metric) > 40 {
			continue
		}
		if err := db.Exec(
			"INSERT INTO health_samples (metric, ts, value) VALUES ($1,$2,$3) ON CONFLICT (metric, ts) DO UPDATE SET value = EXCLUDED.value",
			sm.Metric, sm.TS, sm.Value); err != nil {
			return err
		}
	}
	for _, d := range batch.Days {
		if _, perr := time.Parse("2006-01-02", d.Day); perr != nil {
			continue
		}
		for metric, val := range d.Metrics {
			if len(metric) > 40 {
				continue
			}
			if err := db.Exec(
				"INSERT INTO health_metrics (day, metric, value) VALUES ($1,$2,$3) ON CONFLICT (day, metric) DO UPDATE SET value = EXCLUDED.value",
				d.Day, metric, val); err != nil {
				return err
			}
		}
		// One journal line per day , idempotent, so sleep and movement reach the distiller. The
		// entry states what was measured; interpretation is the distiller's and the check-in's job.
		parts := ""
		if v, ok := d.Metrics["sleep_minutes"]; ok && v > 0 {
			parts += fmt.Sprintf("Slept %dh%02dm. ", int(v)/60, int(v)%60)
		}
		if v, ok := d.Metrics["steps"]; ok && v > 0 {
			parts += fmt.Sprintf("%d steps. ", int(v))
		}
		if v, ok := d.Metrics["exercise_minutes"]; ok && v > 0 {
			parts += fmt.Sprintf("%d min of exercise. ", int(v))
		}
		if v, ok := d.Metrics["distance_km"]; ok && v > 0.1 {
			parts += fmt.Sprintf("%.1f km. ", v)
		}
		if v, ok := d.Metrics["calories"]; ok && v > 0 {
			parts += fmt.Sprintf("%d kcal. ", int(v))
		}
		if v, ok := d.Metrics["hr_avg"]; ok && v > 0 {
			hi := ""
			if m, ok2 := d.Metrics["hr_max"]; ok2 && m > 0 {
				hi = fmt.Sprintf(" (peak %d)", int(m))
			}
			parts += fmt.Sprintf("Avg heart rate %d%s. ", int(v), hi)
		}
		if parts == "" {
			continue
		}
		ts := int64(0)
		if t, terr := time.Parse("2006-01-02", d.Day); terr == nil {
			ts = t.Unix() + 43200 // midday anchor
		}
		if err := db.Exec(
			"INSERT INTO journal_entries (source, ref, ts, title, body, created_at) VALUES ('ghost.tallyd', $1, $2, $3, $4, $5) ON CONFLICT (source, ref) DO NOTHING",
			"health:"+d.Day, ts, "health , "+d.Day, parts, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}
