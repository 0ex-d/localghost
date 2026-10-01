// ghost.tallyd takes the phone's Health Connect readout (steps, sleep, heart rate and the rest,
// a day-batch at a time, spooled by secd into <mount>/tallyd/inbox) into health_metrics and
// health_samples, with a journal line per day for the distiller (internal/tally). Its health line
// says whether that is working: a batch that keeps failing, or an inbox nobody drains, shows on
// Box Status instead of "stub ok".
//
// Runs only while the account is UNLOCKED (data lives on the encrypted volume). Exits cleanly on
// SIGTERM so the supervisor's stop-and-confirm-dead teardown never leaves it holding the mount.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
	"github.com/LocalGhostDao/localghost/server/internal/ghosthealth"
	"github.com/LocalGhostDao/localghost/server/internal/harden"
	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/rotlog"
	"github.com/LocalGhostDao/localghost/server/internal/svcconf"
	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

const service = "ghost.tallyd"

func main() {
	harden.NoDump() // same-user processes cannot read this one through /proc; no core file
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

	ing := &ingestState{}
	srv := ghosthealth.NewServer(service, ghosthealth.ReporterFunc(func() ghosthealth.Health {
		return ing.health()
	}))
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
		// health: what the box holds (days, samples, each metric's newest day and value), what
		// waits in the inbox, and how the last ingest went. `ghost-cli ghost.tallyd health`.
		ctl.Handle("health", func(json.RawMessage) (ctlsock.Response, error) {
			mount := filepath.Dir(runDir)
			out := map[string]any{"ingest": ing.snapshot(), "inbox": inboxDepth(filepath.Join(mount, "tallyd", "inbox"))}
			if cfg, cerr := hw.LoadServicesConfig(mount); cerr == nil {
				db := poltergres.NewReadWrite(hw.SocketForMount(mount), cfg.Postgres.Port, cfg.Postgres.RWUser, cfg.Postgres.RWPass, cfg.Postgres.Name)
				if st, qerr := tally.Query(db); qerr == nil {
					out["stored"] = st
				} else {
					out["storedErr"] = qerr.Error()
				}
			}
			data, _ := json.Marshal(out)
			return ctlsock.Response{OK: true, Data: data}, nil
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
		go healthLoop(ctx, filepath.Dir(runDir), ing, lg)
		lg.Info("health ingestion up", "fn", "main")
	} else {
		ing.note("", errors.New("no run dir: ingestion is off (started by hand without GHOST_RUN_DIR)"), tally.Result{})
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

// ingestState is what the last ingests did, for the health line and the `health` command.
type ingestState struct {
	mu        sync.Mutex
	lastAt    time.Time
	lastFile  string
	lastRes   tally.Result
	lastErr   string
	lastErrAt time.Time
	failing   string // the file that keeps failing, "" when none
	ingested  int    // batches taken since start
}

func (s *ingestState) note(file string, err error, res tally.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.lastErr, s.lastErrAt, s.failing = err.Error(), time.Now(), file
		return
	}
	s.lastAt, s.lastFile, s.lastRes, s.failing = time.Now(), file, res, ""
	s.ingested++
}

func (s *ingestState) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{"batchesSinceStart": s.ingested}
	if !s.lastAt.IsZero() {
		out["lastAt"] = s.lastAt.Unix()
		out["lastFile"] = s.lastFile
		out["last"] = s.lastRes
	}
	if s.lastErr != "" {
		out["lastError"] = s.lastErr
		out["lastErrorAt"] = s.lastErrAt.Unix()
	}
	if s.failing != "" {
		out["failing"] = s.failing
	}
	return out
}

// health: degraded while a batch keeps failing (the phone's uploads are landing and going
// nowhere), OK otherwise, with the last ingest in the detail.
func (s *ingestState) health() ghosthealth.Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failing != "" {
		return ghosthealth.Health{Code: ghosthealth.Degraded, Name: service, Detail: "a health batch keeps failing (" + s.failing + "): " + s.lastErr}
	}
	if s.lastErr != "" && s.lastAt.IsZero() {
		return ghosthealth.Health{Code: ghosthealth.Degraded, Name: service, Detail: s.lastErr}
	}
	d := "no health batch taken since start"
	if !s.lastAt.IsZero() {
		d = fmt.Sprintf("last batch %s ago: %d day(s), %d sample(s), newest %s", time.Since(s.lastAt).Round(time.Minute), s.lastRes.Days, s.lastRes.Samples, s.lastRes.NewestDay)
	}
	return ghosthealth.Health{Code: ghosthealth.OK, Name: service, Detail: d}
}

func inboxDepth(dir string) int {
	es, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range es {
		if !e.IsDir() && !strings.HasSuffix(e.Name(), ".part") {
			n++
		}
	}
	return n
}

// healthLoop drains tallyd's inbox: once at start (a batch that landed while tallyd was down
// used to wait for the first tick), then every 30 s. A batch that fails stays and is tried
// again; the health line says so. A file that is not a batch at all is moved aside, once.
func healthLoop(ctx context.Context, mount string, ing *ingestState, lg *slog.Logger) {
	inbox := filepath.Join(mount, "tallyd", "inbox")
	done := filepath.Join(mount, "tallyd", "done")
	for _, d := range []string{inbox, done} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			lg.Error("inbox dirs", "fn", "healthLoop", "err", err)
			ing.note("", fmt.Errorf("cannot make %s: %v", d, err), tally.Result{})
			return
		}
	}
	var db *poltergres.ReadWrite
	drain := func() {
		entries, err := os.ReadDir(inbox)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasSuffix(e.Name(), ".part") {
				continue
			}
			if db == nil {
				cfg, cerr := hw.LoadServicesConfig(mount)
				if cerr != nil {
					ing.note(e.Name(), fmt.Errorf("services.conf: %v", cerr), tally.Result{})
					break
				}
				db = poltergres.NewReadWrite(hw.SocketForMount(mount), cfg.Postgres.Port,
					cfg.Postgres.RWUser, cfg.Postgres.RWPass, cfg.Postgres.Name)
			}
			path := filepath.Join(inbox, e.Name())
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				ing.note(e.Name(), rerr, tally.Result{})
				continue
			}
			res, ierr := tally.Ingest(db, raw)
			if ierr != nil {
				lg.Warn("health ingest failed, will retry next tick", "fn", "healthLoop", "file", e.Name(), "err", ierr)
				ing.note(e.Name(), ierr, tally.Result{})
				db = nil
				continue
			}
			if res.Unparsable {
				lg.Warn("health batch unparseable, moved aside", "fn", "healthLoop", "file", e.Name())
			} else {
				lg.Info("health batch taken", "fn", "healthLoop", "file", e.Name(), "days", res.Days, "metrics", res.Metrics, "samples", res.Samples, "dropped", res.Dropped, "newest", res.NewestDay)
			}
			ing.note(e.Name(), nil, res)
			_ = os.Rename(path, filepath.Join(done, e.Name()))
		}
		// the raw batches are not kept for ever: the tables hold them now
		pruneDone(done, 200)
	}
	drain()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			drain()
		}
	}
}

// pruneDone keeps the newest keep files under dir.
func pruneDone(dir string, keep int) {
	es, err := os.ReadDir(dir)
	if err != nil || len(es) <= keep {
		return
	}
	names := make([]string, 0, len(es))
	for _, e := range es {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}
