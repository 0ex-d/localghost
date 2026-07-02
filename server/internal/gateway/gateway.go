package gateway

import "errors"

// ghost.secd is the front door for the box. Every other daemon (ghost.tallyd, ghost.voiced,
// ghost.shadowd, ...) listens only on the loopback interface and is never exposed directly. All
// outside traffic terminates at ghost.secd, which authenticates it (mTLS + the unlocked account),
// then proxies to the right daemon for the MOUNTED account. This makes ghost.secd the single trust
// boundary: auth, account selection, and the wipe logic all live at one chokepoint, and a
// daemon can only ever serve the account that is currently unlocked.
//
//	internet ──TLS──> nginx ──> ghost.secd (authn + account routing) ──loopback──> ghost.<x>d
//
// nginx terminates public TLS for the box's domain and forwards to ghost.secd. ghost.secd does the
// real authentication and routing; nginx is just the edge listener and certificate holder.

// Service is a backing daemon ghost.secd proxies to, addressed on loopback. Routes are per service
// name (the "tallyd" in /api/tallyd/...). A service only receives requests once an account is
// mounted, and only ever for that account.
type Service struct {
	Name     string // e.g. "tallyd"
	LoopAddr string // e.g. "127.0.0.1:9310"
}

// Router maps a request to a backing service for the currently mounted account. It refuses
// everything until an account is mounted, so no daemon is reachable from a locked box.
type Router struct {
	services map[string]Service
}

func NewRouter() *Router { return &Router{services: make(map[string]Service)} }

func (r *Router) Register(s Service) { r.services[s.Name] = s }

var (
	ErrLocked     = errors.New("no account mounted; box is locked")
	ErrNoService  = errors.New("unknown service")
)

// Resolve returns the loopback address to proxy to, for a service name, given the mounted account.
// mountedSlot < 0 means locked: refuse. The caller (the proxy handler) has already authenticated
// the request; Resolve only decides routing, and only when unlocked.
func (r *Router) Resolve(mountedSlot int, service string) (string, error) {
	if mountedSlot < 0 {
		return "", ErrLocked
	}
	s, ok := r.services[service]
	if !ok {
		return "", ErrNoService
	}
	return s.LoopAddr, nil
}
