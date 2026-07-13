// ghost.synthd is the memory-surfacing daemon: it owns the retrieval side of the "Before You Ask"
// loop. Given a context (what the user is doing now), it ranks memories from its index and returns
// candidates for ghost.cued to gate. synthd decides WHAT is a candidate; cued decides WHEN and
// WHETHER anything reaches the user.
//
// HONEST STATE. The INDEX , embeddings, vector store, the memory corpus , is the next few months of
// work and does not exist yet. So synthd runs the real query PIPELINE over an EMPTY index: the "prime"
// command executes the whole path and returns nothing, because nothing is indexed. synthd is
// running-but-blind. When the corpus is built behind the Index interface, synthd starts returning
// candidates with no change to cued or to the pipeline.
//
// It exposes its work over the same control socket everything uses: base commands (ping/status/reload/
// log-level) plus its own , prime (the hot path cued calls), ready, and index-stats. So ghost-cli can
// query it (`ghost-cli ghost.synthd index-stats`) just like any other service.
//
// Runs only while UNLOCKED. Spawned by ghost.watchd from <mount>/bin; logs to
// <mount>/logs/ghost.synthd-YYYY-MM-DD.log.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
	"github.com/LocalGhostDao/localghost/server/internal/ghosthealth"
	"github.com/LocalGhostDao/localghost/server/internal/oracle"
	"github.com/LocalGhostDao/localghost/server/internal/rotlog"
	"github.com/LocalGhostDao/localghost/server/internal/svcconf"
	"github.com/LocalGhostDao/localghost/server/internal/synth"
	"github.com/LocalGhostDao/localghost/server/internal/synthd"
)

const service = "ghost.synthd"

func main() {
	port := flag.Int("health-port", envPort("GHOST_HEALTH_PORT"), "loopback health/status port (required)")
	flag.Parse()
	if *port <= 0 {
		log.Fatalf("%s: no health port (set --health-port or GHOST_HEALTH_PORT)", service)
	}

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

	// The query engine over the EMPTY index (the corpus is not built). Runs the full pipeline; returns
	// nothing until the real index slots in behind the Index interface.
	engine := synthd.New(synthd.NewEmptyIndex(), lg)
	if !engine.Ready() {
		lg.Info("running, but the memory index is EMPTY until the corpus is built , prime returns "+
			"nothing (cued stays silent); the query pipeline is live and ready for the index", "fn", "main")
	}

	srv := ghosthealth.NewServer(service, ghosthealth.ReporterFunc(func() ghosthealth.Health {
		d := ""
		if !engine.Ready() {
			d = "index empty (corpus not built)"
		}
		return ghosthealth.Health{Code: ghosthealth.OK, Name: service, Detail: d}
	}))
	go func() {
		if err := srv.Serve(*port); err != nil {
			lg.Error("health server stopped", "fn", "main", "err", err)
		}
	}()

	runDir := os.Getenv("GHOST_RUN_DIR")
	if runDir == "" {
		if ld := os.Getenv("GHOST_LOG_DIR"); ld != "" {
			runDir = filepath.Join(filepath.Dir(ld), "run")
		}
	}
	if runDir != "" {
		mount := filepath.Dir(runDir)
		ctl := ctlsock.NewServer(service, runDir, lg)
		svcconf.BindBase(ctl, service, lvl, func() (svcconf.Base, map[string]string, error) {
			base := svcconf.DefaultBase()
			_ = svcconf.Load(svcconf.Path(mount, service), &base)
			svcconf.FillBaseDefaults(&base)
			return base, nil, nil
		})
		// prime: the hot path cued calls on every context change. Runs the query pipeline.
		ctl.Handle("prime", func(args json.RawMessage) (ctlsock.Response, error) {
			var q synth.Query
			if len(args) > 0 {
				if err := json.Unmarshal(args, &q); err != nil {
					return ctlsock.Response{}, err
				}
			}
			cands, err := engine.Query(context.Background(), q)
			if err != nil {
				return ctlsock.Response{}, err
			}
			data, _ := json.Marshal(cands)
			return ctlsock.Response{OK: true, Data: data}, nil
		})
		// ready: whether the index is actually queryable (cued's SocketClient.Ready reads this).
		ctl.Handle("ready", func(json.RawMessage) (ctlsock.Response, error) {
			data, _ := json.Marshal(map[string]bool{"ready": engine.Ready()})
			return ctlsock.Response{OK: true, Data: data}, nil
		})
		// chat: the app's question -> oracled -> answer. TODAY a PURE PASSTHROUGH , synthd adds no
		// context because the index is empty. This is deliberately the seam where retrieval joins:
		// when the corpus exists, this handler looks up relevant memories and injects them into the
		// prompt before oracled sees it, with zero change to secd or the app. Interactive priority ,
		// a person is waiting.
		oc := oracle.NewClient(runDir, 120*time.Second)
		ctl.Handle("chat", func(args json.RawMessage) (ctlsock.Response, error) {
			var q struct {
				Prompt string `json:"prompt"`
				Think  string `json:"think"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return ctlsock.Response{}, err
			}
			// CONTEXT INJECTION , the seam, now live-but-empty. Ask ghost.searchd for archive matches
			// and prepend them; today the index is empty so this returns nothing and the request is a
			// byte-identical passthrough. The moment framed's captions start flowing through ingest,
			// chat answers become grounded in the archive with no further change here or above.
			// Retrieval is time-boxed and failure is SILENT-but-logged: a slow or dead searchd must
			// never stall or fail a chat that the model alone could answer.
			input := q.Prompt
			if ctxBlock := retrieveContext(runDir, q.Prompt); ctxBlock != "" {
				input = ctxBlock + "\n\nUsing the context above only where it is actually relevant, answer:\n" + q.Prompt
			}
			resp, err := oc.Infer(oracle.Request{
				Capability: "chat",
				Class:      oracle.ClassLocalSmall,
				Priority:   oracle.PriorityInteractive,
				Input:      input,
				Think:      q.Think,
			})
			if err != nil {
				return ctlsock.Response{}, err
			}
			data, _ := json.Marshal(resp)
			return ctlsock.Response{OK: true, Data: data}, nil
		})
		// index-stats: operator view of the corpus (empty today).
		ctl.Handle("index-stats", func(json.RawMessage) (ctlsock.Response, error) {
			data, _ := json.Marshal(map[string]any{"ready": engine.Ready(), "size": engine.Size()})
			return ctlsock.Response{OK: true, Data: data}, nil
		})
		defer ctl.Cleanup()
		go func() {
			if err := ctl.Serve(ctx); err != nil {
				lg.Error("control server exited", "fn", "main", "err", err)
			}
		}()
	}

	lg.Info("up", "fn", "main", "healthPort", *port, "indexReady", engine.Ready())

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

// retrieveContext asks ghost.searchd for archive material matching the prompt and formats it as a
// dated context block. Empty string when there is nothing (empty index, searchd down, timeout) , the
// chat then proceeds exactly as a bare passthrough. 3s budget: retrieval must never hold a person's
// question hostage. Capped small: six snippets is grounding, sixty is noise.
func retrieveContext(runDir, prompt string) string {
	c := ctlsock.NewClientTimeout("ghost.searchd", runDir, 3*time.Second)
	resp, err := c.Call("search", map[string]any{"query": prompt, "limit": 6})
	if err != nil {
		slog.Debug("context retrieval unavailable, chat proceeds bare", "fn", "retrieveContext", "err", err)
		return ""
	}
	var results []struct {
		Label      string   `json:"label"`
		CapturedAt int64    `json:"capturedAt"`
		Snippets   []string `json:"snippets"`
		OrigSource string   `json:"origSource"`
	}
	if err := json.Unmarshal(resp.Data, &results); err != nil || len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Context from the user's personal archive (retrieved automatically, may be irrelevant):")
	n := 0
	for _, r := range results {
		line := r.Label
		for _, sn := range r.Snippets {
			if sn != "" {
				line = sn
				break
			}
		}
		if line == "" {
			continue
		}
		when := ""
		if r.CapturedAt > 0 {
			when = time.Unix(r.CapturedAt, 0).UTC().Format("2006-01-02") + ", "
		}
		src := r.OrigSource
		if src == "" {
			src = "archive"
		}
		b.WriteString("\n- [" + when + src + "] " + line)
		n++
	}
	if n == 0 {
		return ""
	}
	slog.Info("context injected into chat", "fn", "retrieveContext", "snippets", n)
	return b.String()
}
