package secd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/profile"
)

// While MODEL waits, the unlock carries oracled's load progress (phase, percent, time left) for the
// app's bar, and the stage completes when /health turns green.
func TestWaitModelReadyCarriesTheLoad(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			if polls.Load() >= 2 {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "detail": ""})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "detail": "model loading"})
			}
		case "/load":
			n := polls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"phase": "weights", "pct": 30 * n, "etaMs": 9000, "elapsedMs": 4000, "weightsPct": 33})
		}
	}))
	defer srv.Close()
	old := oracledHealthURL
	oracledHealthURL = srv.URL
	defer func() { oracledHealthURL = old }()

	u := newUnlockService(nil)
	var seen []profile.StepState
	var mid *modelLoad
	emit := func(p profile.Progress) {
		if p.Stage == profile.StageModel {
			seen = append(seen, p.State)
		}
	}
	done := make(chan struct{})
	go func() { u.waitModelReady(emit, 20*time.Second); close(done) }()
	deadline := time.After(10 * time.Second)
	for mid == nil {
		select {
		case <-deadline:
			t.Fatal("no progress carried")
		case <-time.After(100 * time.Millisecond):
		}
		u.mu.Lock()
		if u.model != nil && u.model.Phase == "weights" {
			m := *u.model
			mid = &m
		}
		u.mu.Unlock()
	}
	<-done
	if mid.Pct != 30 || mid.EtaMs != 9000 {
		t.Fatalf("mid-load %+v", *mid)
	}
	if len(seen) != 2 || seen[0] != profile.Running || seen[1] != profile.Complete {
		t.Fatalf("stage states %v", seen)
	}
	if u.model == nil || u.model.Phase != "ready" || u.model.Pct != 100 {
		t.Fatalf("at the end %+v", u.model)
	}
}

// A load that failed and did not start again ends the wait without waiting out the deadline.
func TestWaitModelReadyStopsOnAFailedLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("takes 40 s")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "detail": "model not running"})
		case "/load":
			_ = json.NewEncoder(w).Encode(map[string]any{"phase": "failed", "pct": 12, "etaMs": -1})
		}
	}))
	defer srv.Close()
	old := oracledHealthURL
	oracledHealthURL = srv.URL
	defer func() { oracledHealthURL = old }()
	u := newUnlockService(nil)
	var last profile.StepState
	t0 := time.Now()
	u.waitModelReady(func(p profile.Progress) { last = p.State }, 3*time.Minute)
	if last != profile.Errored || time.Since(t0) > 60*time.Second {
		t.Fatalf("state %v after %s", last, time.Since(t0))
	}
}
