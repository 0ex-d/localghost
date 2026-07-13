package secd

// /v1/chat , the app's question, answered by the box's own model. secd forwards to ghost.synthd's
// chat command (the retrieval seam: today a pure passthrough to ghost.oracled, tomorrow the place
// where the memory index injects context), and returns the answer. Session-authenticated, appears-
// down on rejection like every other route. Non-streaming v1: the model runs to completion, then one
// JSON reply , llama-server streaming can be plumbed later without changing this route's shape.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/LocalGhostDao/localghost/server/internal/streamsock"
	"time"

)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) {
		secdLog.Warn("chat rejected: invalid session", "fn", "handleChat", "bearerPresent", bearer(r) != "")
		s.appearsDown(w)
		return
	}
	if r.Method != http.MethodPost {
		s.appearsDown(w)
		return
	}
	s.mu.Lock()
	mounted := s.mounted
	s.mu.Unlock()
	if mounted < 0 {
		s.appearsDown(w)
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
		Think  string `json:"think"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		s.appearsDown(w)
		return
	}
	// STREAMING: pipe synthd's event stream (context first, then tokens, then done) straight to the
	// app. secd adds authentication and appears-down; it does not touch the events. Cancellation
	// flows: app hangs up -> this request context cancels -> synthd -> oracled -> llama stops
	// generating, so an abandoned question stops burning CPU.
	body, _ := json.Marshal(map[string]string{"prompt": req.Prompt, "think": req.Think})
	runDir := fmt.Sprintf("%s/mnt/slot%d/run", s.cfg.StateDir, mounted)
	up, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"http://ghost/chat", bytes.NewReader(body))
	if err != nil {
		s.appearsDown(w)
		return
	}
	up.Header.Set("Content-Type", "application/json")
	t0 := time.Now()
	resp, err := streamsock.Client("ghost.synthd", runDir).Do(up)
	if err != nil {
		secdLog.Warn("chat failed: synthd unreachable", "fn", "handleChat", "err", err)
		s.appearsDown(w) // model down / loading / synthd absent: indistinguishable from box-down, by design
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		secdLog.Warn("chat failed", "fn", "handleChat", "code", resp.StatusCode)
		s.appearsDown(w)
		return
	}
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return // app hung up , context cancellation stops the chain
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
	secdLog.Info("chat streamed", "fn", "handleChat", "took", time.Since(t0).Round(time.Millisecond).String())
}

