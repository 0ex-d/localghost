// Package rotlog is a midnight-rotating log writer plus a standard slog logger built on it. A process
// writes through it for years: it holds ONE open file for the whole day and, on the first write after
// local midnight, closes yesterday's file and opens today's. Every other write is a bare append , the
// only per-write cost is a date-string compare (a few ns), so there is no timer, no goroutine, and no
// restart. A daemon that runs for four years rolls a file per day on its own.
//
// The service name and date are NOT in each line , they are in the filename (<name>-YYYY-MM-DD.log),
// so a line only carries the intraday clock and its structured fields. watchd's Roller archives
// completed days (gzip) and prunes to the last 7.
package rotlog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RotWriter is an io.Writer targeting <dir>/<name>-YYYY-MM-DD.log. It keeps the current day's file
// OPEN and only swaps it when the day actually changes , it does not reopen per write. Safe for
// concurrent writers (a daemon's stdout and stderr, or many goroutines, may share one).
type RotWriter struct {
	mu   sync.Mutex
	dir  string
	name string
	day  string   // date of the file currently held open
	f    *os.File // the one open fd for `day`
}

// New opens today's file for name under dir.
func New(dir, name string) (*RotWriter, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	w := &RotWriter{dir: dir, name: name}
	if err := w.openDay(today()); err != nil {
		return nil, err
	}
	return w, nil
}

func today() string { return time.Now().Format("2006-01-02") }

func (w *RotWriter) path(day string) string {
	return filepath.Join(w.dir, fmt.Sprintf("%s-%s.log", w.name, day))
}

// openDay closes the currently-held file (if any) and opens the given day's. Called once at start and
// once per day boundary , never on a same-day write.
func (w *RotWriter) openDay(day string) error {
	f, err := os.OpenFile(w.path(day), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	if w.f != nil {
		_ = w.f.Close()
	}
	w.f, w.day = f, day
	return nil
}

// Write compares today's date to the held file's day. Equal (the all-day case) -> straight append to
// the open fd. Different (once, just after midnight) -> swap to the new day's file, then append. One
// open file per day, held all day; no reopen churn.
func (w *RotWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if d := today(); d != w.day {
		if err := w.openDay(d); err != nil {
			return 0, err
		}
	}
	return w.f.Write(p)
}

// Close closes the held file. The writer is unusable afterward.
func (w *RotWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Logger builds a slog.Logger writing key=value lines through w. svc and date are NOT in the line ,
// the filename carries them. Time is intraday HH:MM:SS.ns (UTC), high-res so ordering within a busy
// second is exact. Callers add fn= per call site.
// Output: 15:04:05.123456789 level=INFO fn=Produce msg="stored notification" id=4127
//
// The level is read from GHOST_LOG_LEVEL (debug|info|warn|error, default info), so a box can be run
// verbose without a code change , set GHOST_LOG_LEVEL=debug and every Debug line in the code shows up.
// The returned LevelVar is the live level: a caller can raise or lower it at runtime (e.g. over the
// control socket) with no restart, which matters for daemons that run for years. Callers that do not
// need live control can ignore it.
func Logger(w io.Writer) (*slog.Logger, *slog.LevelVar) {
	lvl := new(slog.LevelVar)
	lvl.Set(LevelFromEnv())
	h := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(time.Now().UTC().Format("15:04:05.000000000"))
			}
			return a
		},
	})
	return slog.New(h), lvl
}

// LevelFromEnv maps GHOST_LOG_LEVEL to a slog.Level, defaulting to Info. Exported so secd (which logs
// to journald, not a rotlog file) can build a handler at the same level with the same env contract.
func LevelFromEnv() slog.Level {
	switch strings.ToLower(os.Getenv("GHOST_LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
