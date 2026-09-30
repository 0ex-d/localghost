package secd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// /v1/model's state: loading with oracled's own progress, then ready; "starting" when oracled
// does not answer.
func TestModelStatus(t *testing.T) {
	var ready atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			if ready.Load() {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "detail": "model loading"})
			}
		case "/load":
			_ = json.NewEncoder(w).Encode(map[string]any{"phase": "weights", "pct": 42, "etaMs": 9000, "elapsedMs": 4000})
		}
	}))
	old := oracledHealthURL
	oracledHealthURL = srv.URL
	defer func() { oracledHealthURL = old }()
	c := &http.Client{Timeout: time.Second}

	st := modelStatus(c)
	if st.Ready || st.Phase != "weights" || st.Pct != 42 || st.EtaMs != 9000 || st.Detail != "model loading" {
		t.Fatalf("loading: %+v", st)
	}
	ready.Store(true)
	if st = modelStatus(c); !st.Ready || st.Pct != 100 {
		t.Fatalf("ready: %+v", st)
	}
	srv.Close()
	if st = modelStatus(c); st.Ready || st.Phase != "starting" {
		t.Fatalf("no oracled: %+v", st)
	}
}
