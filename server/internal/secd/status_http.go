package secd

// /v1/status , the supervised-daemon roster for the app's Box Status screen. secd does not supervise
// the cohort itself (ghost.watchd does); this proxies watchd's snapshot, fetched over the control
// socket by the backend, into the {"services":[...]} shape the app parses. Session-authenticated and
// appears-down on every rejection, exactly like the other authenticated endpoints , a locked box or a
// missing session is indistinguishable from the box being down.

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) {
		secdLog.Warn("status rejected: invalid session", "fn", "handleStatus", "bearerPresent", bearer(r) != "")
		s.appearsDown(w)
		return
	}
	if r.Method != http.MethodGet {
		secdLog.Warn("status rejected: wrong method", "fn", "handleStatus", "method", r.Method)
		s.appearsDown(w)
		return
	}
	s.mu.Lock()
	mounted := s.mounted
	s.mu.Unlock()
	if mounted < 0 {
		secdLog.Warn("status rejected: box locked", "fn", "handleStatus")
		s.appearsDown(w) // locked: no daemons to report, and we do not reveal lock state anyway
		return
	}
	// watchd's live snapshot , the same data ghost-cli and health.sh see.
	services := s.unlock.SupervisorStatus()
	if services == nil {
		services = []ServiceStatus{} // never null: the app expects an array, empty means "none reported"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"services": services})
}
