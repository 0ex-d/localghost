package oracled

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LOAD PROGRESS , how far a start of llama-server has got, for the unlock screen's bar. Read from
// llama-server's own output, not guessed from a clock:
//
//   - starting: spawned, nothing about the model said yet
//   - weights:  llama.cpp reading the model file. It prints one "." per percent of the tensors
//     loaded (its default progress callback, on a line of its own), so WeightsPct is llama.cpp's
//     own count, not an estimate.
//   - finishing: the weights are in; the context is being set up
//   - projector: the multimodal projector (clip/mtmd lines), after the weights
//   - warmup:   "warming up the model", the last step before /health answers
//   - ready | failed
//
// The time left comes from two measurements: the rate the dots are arriving at now, and how long
// the part after the weights took the last time this box loaded (kept in LoadTimes on the volume).
// Before a first complete load there is no history, and the estimate says so (EtaMs -1).

// LoadProgress is what /load on oracled's health port answers, and what secd passes to the app.
type LoadProgress struct {
	Phase      string `json:"phase"`
	Pct        int    `json:"pct"`        // 0..100 overall, never goes backwards within a start
	WeightsPct int    `json:"weightsPct"` // llama.cpp's own count of the tensors loaded
	ElapsedMs  int64  `json:"elapsedMs"`
	EtaMs      int64  `json:"etaMs"` // -1 = no basis for an estimate yet
	LastMs     int64  `json:"lastMs,omitempty"`
	Try        int    `json:"try,omitempty"`
}

// LoadTimes is one complete load, measured, kept for the next estimate.
type LoadTimes struct {
	PreMs     int64 `json:"preMs"`     // spawn to the first sign of the weights
	WeightsMs int64 `json:"weightsMs"` // the weights, first sign to the last dot
	PostMs    int64 `json:"postMs"`    // after the weights to ready (projector, warm-up)
	TotalMs   int64 `json:"totalMs"`
	At        int64 `json:"at"` // unix seconds
}

type loadTracker struct {
	mu        sync.Mutex
	path      string
	last      LoadTimes
	phase     string
	started   time.Time
	weightsAt time.Time // first sign of the weights
	weightsIn time.Time // the last dot
	dots      int
	try       int
	maxPct    int
	now       func() time.Time
}

func newLoadTracker(path string) *loadTracker {
	t := &loadTracker{path: path, phase: "idle", now: time.Now}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &t.last)
		}
	}
	return t
}

// begin marks a new start of the child.
func (t *loadTracker) begin(try int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.phase, t.started, t.try = "starting", t.now(), try
	t.weightsAt, t.weightsIn = time.Time{}, time.Time{}
	t.dots, t.maxPct = 0, 0
}

// line reads one complete line of the child's output.
func (t *loadTracker) line(l string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase == "idle" || t.phase == "ready" || t.phase == "failed" {
		return
	}
	low := strings.ToLower(l)
	switch {
	case isDotRun(l) && dotCount(l) >= 50:
		// the progress line, finished: the weights are in; what follows is the projector (when
		// there is one), the context and the warm-up
		t.dots = 100
		t.markWeightsIn()
		if t.phase == "weights" || t.phase == "starting" {
			t.phase = "finishing"
		}
	case strings.Contains(low, "warming up"):
		t.markWeightsIn()
		t.phase = "warmup"
	case strings.Contains(low, "clip_model_loader") || strings.Contains(low, "clip_ctx") || strings.Contains(low, "mtmd") || strings.Contains(low, "load_hparams"):
		// the projector, once the weights are in (a build that reads it first is still loading weights)
		if t.phase == "finishing" {
			t.phase = "projector"
		}
	case strings.Contains(low, "llama_model_load") || strings.Contains(low, "llama_model_loader") || strings.Contains(low, "load_tensors"):
		if t.phase == "starting" {
			t.phase = "weights"
			t.weightsAt = t.now()
		}
	}
}

func (t *loadTracker) markWeightsIn() {
	if t.weightsIn.IsZero() {
		t.weightsIn = t.now()
		if t.weightsAt.IsZero() {
			t.weightsAt = t.started
		}
	}
}

// partial reads the unfinished line: llama.cpp's dots arrive one per percent with no newline until
// the last.
func (t *loadTracker) partial(p []byte) {
	if len(p) == 0 {
		return
	}
	s := string(p)
	if !isDotRun(s) {
		return
	}
	n := dotCount(s)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase == "starting" {
		t.phase = "weights"
		t.weightsAt = t.now()
	}
	if t.phase == "weights" && n > t.dots {
		if n > 100 {
			n = 100
		}
		t.dots = n
	}
}

// ready: /health answered. The load's times are kept for the next estimate.
func (t *loadTracker) ready() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.phase == "idle" || t.started.IsZero() {
		return
	}
	now := t.now()
	t.markWeightsIn()
	lt := LoadTimes{
		PreMs:     t.weightsAt.Sub(t.started).Milliseconds(),
		WeightsMs: t.weightsIn.Sub(t.weightsAt).Milliseconds(),
		PostMs:    now.Sub(t.weightsIn).Milliseconds(),
		TotalMs:   now.Sub(t.started).Milliseconds(),
		At:        now.Unix(),
	}
	t.phase, t.dots, t.maxPct = "ready", 100, 100
	t.last = lt
	if t.path != "" {
		if b, err := json.Marshal(lt); err == nil {
			_ = os.MkdirAll(filepath.Dir(t.path), 0o750)
			_ = os.WriteFile(t.path+".tmp", b, 0o640)
			_ = os.Rename(t.path+".tmp", t.path)
		}
	}
}

func (t *loadTracker) fail() {
	t.mu.Lock()
	t.phase = "failed"
	t.mu.Unlock()
}

// snapshot is the progress now: the phase, the percent and the time left.
func (t *loadTracker) snapshot() LoadProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := LoadProgress{Phase: t.phase, WeightsPct: t.dots, EtaMs: -1, LastMs: t.last.TotalMs, Try: t.try}
	switch t.phase {
	case "idle":
		return p
	case "ready":
		p.Pct, p.EtaMs, p.ElapsedMs = 100, 0, t.last.TotalMs
		return p
	}
	now := t.now()
	elapsed := now.Sub(t.started)
	p.ElapsedMs = elapsed.Milliseconds()
	last := t.last
	post := time.Duration(last.PostMs) * time.Millisecond
	var eta time.Duration = -1
	switch t.phase {
	case "starting":
		if last.TotalMs > 0 {
			eta = time.Duration(last.TotalMs)*time.Millisecond - elapsed
		}
	case "weights":
		inW := now.Sub(t.weightsAt)
		if t.dots >= 3 && inW > 0 {
			// the rate the dots come at now, and the rest after the weights as it was last time
			rest := time.Duration(float64(inW) * float64(100-t.dots) / float64(t.dots))
			eta = rest + post
			if last.TotalMs == 0 {
				eta = rest + 3*time.Second // no history: a few seconds for the projector and warm-up
			}
		} else if last.TotalMs > 0 {
			eta = time.Duration(last.WeightsMs+last.PostMs)*time.Millisecond - inW
		}
	case "finishing", "projector", "warmup":
		if last.PostMs > 0 {
			eta = post - now.Sub(t.weightsIn)
		}
	case "failed":
		eta = -1
	}
	if eta != -1 {
		if eta < time.Second {
			eta = time.Second // not done until /health says so
		}
		p.EtaMs = eta.Milliseconds()
	}
	// the percent: time done over time done plus time left when there is an estimate, else the
	// phase's share with llama.cpp's own count inside the weights
	pct := 0
	switch {
	case p.EtaMs > 0:
		pct = int(100 * float64(p.ElapsedMs) / float64(p.ElapsedMs+p.EtaMs))
	case t.phase == "starting":
		pct = 3
	case t.phase == "weights":
		pct = 5 + t.dots*85/100
	case t.phase == "finishing", t.phase == "projector":
		pct = 92
	case t.phase == "warmup":
		pct = 96
	}
	if pct > 99 {
		pct = 99
	}
	if pct < t.maxPct {
		pct = t.maxPct // a bar that goes backwards is worse than one that pauses
	}
	t.maxPct = pct
	p.Pct = pct
	return p
}

func isDotRun(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Trim(s, ".") == ""
}

func dotCount(s string) int { return strings.Count(s, ".") }
