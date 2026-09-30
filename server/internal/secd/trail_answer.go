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

// handleTrailForget , POST /v1/geo/trail/forget {"ts","radiusM","dry"} , {"ok","ts":[...],"deleted"}.
// The person zoomed in on one fix and said it is wrong: ghost.framed takes that fix and its
// neighbours at the same spot (framed/forget.go). dry answers which, without deleting, so the
// phone can say "4 fixes, 14:10 to 15:00" before the person confirms.
func (s *Server) handleTrailForget(w http.ResponseWriter, r *http.Request) {
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
		TS      int64   `json:"ts"`
		RadiusM float64 `json:"radiusM"`
		Dry     bool    `json:"dry"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req) != nil || req.TS <= 0 || req.RadiusM < 0 || req.RadiusM > 1000 {
		http.Error(w, "ts, radiusM (0..1000) and dry", http.StatusBadRequest)
		return
	}
	runDir := fmt.Sprintf("%s/mnt/slot%d/run", s.cfg.StateDir, mounted)
	c := ctlsock.NewClientTimeout("ghost.framed", runDir, 30*time.Second)
	resp, err := c.Call("trail-forget", req)
	if err != nil || !resp.OK {
		why := ""
		if err != nil {
			why = err.Error()
		} else {
			why = resp.Err
		}
		secdLog.Warn("trail forget failed", "fn", "handleTrailForget", "err", why)
		http.Error(w, "framed did not take it", http.StatusServiceUnavailable)
		return
	}
	var out struct {
		TS      []int64 `json:"ts"`
		Deleted int     `json:"deleted"`
	}
	_ = json.Unmarshal(resp.Data, &out)
	if out.TS == nil {
		out.TS = []int64{}
	}
	writeJSON(w, map[string]any{"ok": true, "ts": out.TS, "deleted": out.Deleted})
}
