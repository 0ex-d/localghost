package secd

// ServiceStatus is secd's view of one supervised daemon, as shown on /v1/status. secd no longer
// supervises the daemons itself (ghost.watchd does); this type is populated from watchd's snapshot
// fetched over the control socket (see backend.SupervisorStatus). Kept in the secd package because
// it is part of the /v1/status response shape the app consumes.
type ServiceStatus struct {
	Name     string `json:"name"`
	Critical bool   `json:"critical"`
	State    string `json:"state"`
	Restarts int    `json:"restarts"`
	LastErr  string `json:"lastErr,omitempty"`
	Code     uint8  `json:"code"`
}
