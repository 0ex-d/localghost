package watchd

// watchd is the janitor for the daemon logs. Rotation itself is NOT done here , each daemon (and
// watchd) writes through a self-rotating rotlog.RotWriter that opens a new <name>-YYYY-MM-DD.log at
// midnight on its own, with no restart and no supervisor in the write path. watchd's Roller only does
// the housekeeping a long-running writer cannot do for itself:
//   - at midnight, gzip every COMPLETED day's <name>-YYYY-MM-DD.log into archive/<same>.gz and remove
//     the plain file (today's open files are left alone),
//   - keep the last 7 days of archives per daemon and delete older ones.
//
// It runs while watchd is up (i.e. while the box is unlocked); a locked box has no volume to log to.

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const retentionDays = 7

// Roller does the daily archive + prune for a logDir, and (if set) runs the disk-guard check after
// each pass. One per watchd process.
type Roller struct {
	logDir string
	guard  *DiskGuard // optional; when set, Check() runs after archive+prune
}

func NewRoller(logDir string) *Roller { return &Roller{logDir: logDir} }

// WithGuard attaches a disk-guard so the roller enforces the log-folder caps (and asks oracle) on each
// pass. Returns the roller for chaining.
func (r *Roller) WithGuard(g *DiskGuard) *Roller {
	r.guard = g
	return r
}

// Run blocks until stop is closed, waking at each local midnight to archive the completed day and
// prune. It also archives + prunes once at start, catching anything a previous run left behind (e.g.
// yesterday's files if watchd was down at midnight). Intended to run in its own goroutine.
func (r *Roller) Run(stop <-chan struct{}) {
	r.archiveCompletedDays()
	r.prune()
	r.checkGuard()

	// Two cadences: archive+prune at midnight (daily), and the disk-guard every 15 minutes (a runaway
	// logger fills the volume in hours, not days, so the guard cannot wait for midnight).
	guardTick := time.NewTicker(15 * time.Minute)
	defer guardTick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-time.After(untilNextMidnight()):
			// A moment past midnight: yesterday's files are now complete. The daemons have already
			// (or will, on their next write) rolled to today's file themselves; we gzip the finished
			// days regardless of whether a given daemon has written today yet.
			r.archiveCompletedDays()
			r.prune()
			r.checkGuard()
		case <-guardTick.C:
			r.checkGuard()
		}
	}
}

func (r *Roller) checkGuard() {
	if r.guard != nil {
		r.guard.Check()
	}
}

// archiveCompletedDays gzips every <name>-YYYY-MM-DD.log whose day is BEFORE YESTERDAY into
// archive/<same>.gz and removes the plain file. Today's file is open; yesterday's is left PLAIN on
// purpose , that gives an always-available window of uncompressed logs between 24 and 48 hours old, so
// the most recent complete day can be grepped without un-gzipping. Only the day-before-yesterday and
// older get compressed.
func (r *Roller) archiveCompletedDays() {
	archDir := filepath.Join(r.logDir, "archive")
	if err := os.MkdirAll(archDir, 0o750); err != nil {
		return
	}
	entries, err := os.ReadDir(r.logDir)
	if err != nil {
		return
	}
	// keepPlain = today and yesterday; anything strictly older is archived.
	td := today()
	yd := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".log") {
			continue
		}
		day := dayFromLogName(n)
		if day == "" || day == td || day == yd {
			continue // not a dated log, or it is today's/yesterday's , leave it plain
		}
		src := filepath.Join(r.logDir, n)
		dst := filepath.Join(archDir, n+".gz")
		if err := gzipFile(src, dst); err == nil {
			_ = os.Remove(src)
		}
	}
}

// prune deletes archives older than retentionDays: anything past the date cutoff outright, and beyond
// that keeps at most retentionDays newest per daemon.
func (r *Roller) prune() {
	archDir := filepath.Join(r.logDir, "archive")
	entries, err := os.ReadDir(archDir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	byName := map[string][]string{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".log.gz") {
			continue
		}
		day := dayFromLogName(strings.TrimSuffix(n, ".gz"))
		if day == "" {
			continue
		}
		if t, err := time.Parse("2006-01-02", day); err == nil && t.Before(cutoff) {
			_ = os.Remove(filepath.Join(archDir, n))
			continue
		}
		// base = the daemon name: strip ".log.gz" (7) + "YYYY-MM-DD" (10) + "-" (1) = 18 from the end.
		if len(n) < 18 {
			continue
		}
		base := n[:len(n)-18]
		byName[base] = append(byName[base], n)
	}
	for _, files := range byName {
		if len(files) <= retentionDays {
			continue
		}
		sort.Strings(files) // dated names sort chronologically; oldest first
		for _, old := range files[:len(files)-retentionDays] {
			_ = os.Remove(filepath.Join(archDir, old))
		}
	}
}

// --- helpers ---

func untilNextMidnight() time.Duration {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
	return time.Until(next)
}

func today() string { return time.Now().Format("2006-01-02") }

// dayFromLogName pulls YYYY-MM-DD out of "<name>-YYYY-MM-DD.log". Returns "" if it does not match.
func dayFromLogName(n string) string {
	n = strings.TrimSuffix(n, ".log")
	if len(n) < 10 {
		return ""
	}
	day := n[len(n)-10:]
	if len(day) == 10 && day[4] == '-' && day[7] == '-' {
		return day
	}
	return ""
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := gzip.NewWriter(out)
	if _, err := io.Copy(zw, in); err != nil {
		_ = zw.Close()
		return err
	}
	return zw.Close()
}
