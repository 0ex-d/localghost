package cued

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/synth"
)

// fakeSynth returns a fixed candidate set; Ready is configurable.
type fakeSynth struct {
	cands []synth.Candidate
	ready bool
}

func (f *fakeSynth) Prime(context.Context, synth.Query) ([]synth.Candidate, error) {
	return f.cands, nil
}
func (f *fakeSynth) Ready() bool { return f.ready }

// recordSurfacer records what got surfaced.
type recordSurfacer struct {
	mu       sync.Mutex
	surfaced []synth.Candidate
}

func (r *recordSurfacer) Surface(c synth.Candidate) error {
	r.mu.Lock()
	r.surfaced = append(r.surfaced, c)
	r.mu.Unlock()
	return nil
}
func (r *recordSurfacer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.surfaced)
}

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestBelowThresholdStaysSilent: a candidate that does not clear the bar surfaces nothing , the
// intended default (executive suppression).
func TestBelowThresholdStaysSilent(t *testing.T) {
	fs := &fakeSynth{cands: []synth.Candidate{
		{ID: "a", Relevance: 0.4, Distinctiveness: 0.4}, // combined 0.4, below 0.8
	}}
	rec := &recordSurfacer{}
	e := New(DefaultConfig(), fs, rec, testLog())
	e.UpdateContext("morning, kitchen", map[string]string{"place": "kitchen"})
	if rec.count() != 0 {
		t.Fatalf("expected silence below threshold, got %d surfaced", rec.count())
	}
}

// TestClearsThresholdSurfacesOnce: a strong, distinctive candidate surfaces exactly once.
func TestClearsThresholdSurfacesOnce(t *testing.T) {
	fs := &fakeSynth{cands: []synth.Candidate{
		{ID: "b", Title: "x", Relevance: 0.95, Distinctiveness: 0.9}, // combined 0.93 > 0.8
	}}
	rec := &recordSurfacer{}
	cfg := DefaultConfig()
	cfg.Cooldown = 0 // isolate the threshold logic
	e := New(cfg, fs, rec, testLog())
	e.UpdateContext("a", map[string]string{"k": "1"})
	if rec.count() != 1 {
		t.Fatalf("expected one surface, got %d", rec.count())
	}
}

// TestCooldownSuppressesSecond: even a strong candidate does not surface again within the cooldown.
func TestCooldownSuppressesSecond(t *testing.T) {
	fs := &fakeSynth{cands: []synth.Candidate{
		{ID: "c", Relevance: 0.95, Distinctiveness: 0.9},
	}}
	rec := &recordSurfacer{}
	cfg := DefaultConfig()
	cfg.Cooldown = time.Hour
	e := New(cfg, fs, rec, testLog())
	e.UpdateContext("a", map[string]string{"k": "1"})
	e.UpdateContext("b", map[string]string{"k": "2"}) // different context, still within cooldown
	if rec.count() != 1 {
		t.Fatalf("cooldown should suppress the second cue, got %d", rec.count())
	}
}

// TestLearningCurveDecays: a repeatedly-surfaced cue eventually drops below the bar, even though its
// raw relevance is unchanged , the Fitts-Posner pull-back.
func TestLearningCurveDecays(t *testing.T) {
	// a candidate just over the bar: combined 0.6*0.9+0.4*0.85 = 0.88.
	c := synth.Candidate{ID: "d", Relevance: 0.9, Distinctiveness: 0.85}
	fs := &fakeSynth{cands: []synth.Candidate{c}}
	rec := &recordSurfacer{}
	cfg := DefaultConfig()
	cfg.Cooldown = 0            // no cooldown so only the curve suppresses
	cfg.LearningHalfLife = 1    // fast decay: halves each surface
	e := New(cfg, fs, rec, testLog())

	surfaced := 0
	for i := 0; i < 10; i++ {
		before := rec.count()
		e.UpdateContext("ctx", map[string]string{"i": itoa(i)})
		if rec.count() > before {
			surfaced++
		}
	}
	// It should surface a few times then stop as the decayed score falls under 0.8, NOT all 10.
	if surfaced == 0 {
		t.Fatal("expected the cue to surface at least once before decaying")
	}
	if surfaced >= 10 {
		t.Fatalf("learning curve should have suppressed the cue; it surfaced %d/10", surfaced)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
