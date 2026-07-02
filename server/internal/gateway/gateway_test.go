package gateway

import "testing"

func router() *Router {
	r := NewRouter()
	r.Register(Service{Name: "tallyd", LoopAddr: "127.0.0.1:9310"})
	r.Register(Service{Name: "voiced", LoopAddr: "127.0.0.1:9320"})
	return r
}

func TestLockedBoxRefuses(t *testing.T) {
	if _, err := router().Resolve(-1, "tallyd"); err != ErrLocked {
		t.Fatalf("locked box must refuse routing, got %v", err)
	}
}

func TestUnknownServiceRejected(t *testing.T) {
	if _, err := router().Resolve(0, "nope"); err != ErrNoService {
		t.Fatalf("unknown service must be rejected, got %v", err)
	}
}

func TestMountedRoutesToLoopback(t *testing.T) {
	addr, err := router().Resolve(0, "tallyd")
	if err != nil || addr != "127.0.0.1:9310" {
		t.Fatalf("want loopback addr, got %q %v", addr, err)
	}
}

func TestMountedAccountRoutes(t *testing.T) {
	// The mounted account (slot 0) routes to its daemons.
	if _, err := router().Resolve(0, "voiced"); err != nil {
		t.Fatalf("the mounted account must route: %v", err)
	}
}
