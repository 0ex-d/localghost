package secd

// /v1/chat , the app's question, answered by the box's own model. secd forwards to ghost.synthd's
// chat command (the retrieval seam: today a pure passthrough to ghost.oracled, tomorrow the place
// where the memory index injects context), and returns the answer. Session-authenticated, appears-
// down on rejection like every other route. Non-streaming v1: the model runs to completion, then one
// JSON reply , llama-server streaming can be plumbed later without changing this route's shape.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
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
	// Generous client timeout: a deep-think answer on CPU is legitimately minutes, and the app shows
	// its own progress. The unix-socket hop is local; the time is all model.
	runDir := fmt.Sprintf("%s/mnt/slot%d/run", s.cfg.StateDir, mounted)
	c := ctlsock.NewClientTimeout("ghost.synthd", runDir, 5*time.Minute)
	t0 := time.Now()
	resp, err := c.Call("chat", map[string]string{"prompt": req.Prompt, "think": req.Think})
	if err != nil {
		secdLog.Warn("chat failed", "fn", "handleChat", "err", err)
		s.appearsDown(w) // model down / loading / synthd absent: indistinguishable from box-down, by design
		return
	}
	secdLog.Info("chat answered", "fn", "handleChat", "took", time.Since(t0).Round(time.Millisecond).String())
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(resp.Data) // oracle.Response JSON: {output, model}
}
