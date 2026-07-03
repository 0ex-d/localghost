package secd

import "net/http"

// handleStatus serves the authenticated Ghost Status view: per-service supervisor state (up/degraded/
// failed, restart count, last error) for the mounted account. This is NOT gated by the appears-down
// 503 , that is only for the unauthenticated edge. Past a valid session, the owner sees honest health,
// including which capabilities are erroring, rather than a blank down box.
//
// watchd's sampled load metrics (from Redis) will be MERGED in here once watchd is writing them; the
// supervisor state below is the half secd owns (only secd knows restart history and critical-flag
// failures). Shape is pinned by openapi_test against statusDoc.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) {
		s.appearsDown(w) // no session -> looks down, same as everything else
		return
	}
	s.mu.Lock()
	mounted := s.mounted
	s.mu.Unlock()

	writeJSON(w, statusDoc{
		Mounted:  mounted >= 0,
		Services: s.unlock.SupervisorStatus(),
	})
}
