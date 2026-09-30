package secd

// TRAIL QUESTIONS, ANSWERED. A day's trail may carry questions ("the trail goes to Igoumenitsa,
// 38 km away, and is back 12 minutes later: were you there?", framed/questions.go). The phone sends
// the answer here and secd hands it to ghost.framed, which owns the points: yes is remembered and
// never asked again; no deletes those points from the database for good, and the day is drawn,
// measured and told again without them.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
)

// handleTrailAnswer , POST /v1/geo/trail/answer {"from","to","ts":[...],"keep"} , {"ok","deleted"}.
func (s *Server) handleTrailAnswer(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodPost {
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
		From int64   `json:"from"`
		To   int64   `json:"to"`
		TS   []int64 `json:"ts"`
		Keep bool    `json:"keep"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req) != nil ||
		req.From <= 0 || req.To < req.From || len(req.TS) > 500 || (!req.Keep && len(req.TS) == 0) {
		http.Error(w, "from, to, ts and keep", http.StatusBadRequest)
		return
	}
	runDir := fmt.Sprintf("%s/mnt/slot%d/run", s.cfg.StateDir, mounted)
	c := ctlsock.NewClientTimeout("ghost.framed", runDir, 30*time.Second) // rebuilding a day routes it again
	resp, err := c.Call("trail-answer", req)
	if err != nil || !resp.OK {
		why := ""
		if err != nil {
			why = err.Error()
		} else {
			why = resp.Err
		}
		secdLog.Warn("trail answer failed", "fn", "handleTrailAnswer", "err", why)
		http.Error(w, "framed did not take the answer", http.StatusServiceUnavailable)
		return
	}
	var out struct {
		Deleted int `json:"deleted"`
	}
	_ = json.Unmarshal(resp.Data, &out)
	writeJSON(w, map[string]any{"ok": true, "deleted": out.Deleted})
}
