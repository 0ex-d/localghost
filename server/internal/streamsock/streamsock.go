// Package streamsock is HTTP over UNIX SOCKETS for daemon-to-daemon STREAMING. ctlsock stays the
// command protocol (one JSON in, one JSON out); this exists for the one thing it cannot carry ,
// long-lived token streams (chat). Unix sockets over loopback TCP for the same reasons ctlsock
// chose them: filesystem permissions instead of "anything on localhost", no TCP surface at all,
// and sockets that die with the run dir (which dies with the mount).
package streamsock

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// sockPath: <runDir>/<service>.stream.sock , beside the service's ctlsock.
func sockPath(service, runDir string) string {
	return filepath.Join(runDir, service+".stream.sock")
}

// Serve binds the service's stream socket and serves mux on it until the process exits. Stale
// sockets from a previous life are removed first (the bind would otherwise fail after a crash).
func Serve(service, runDir string, mux http.Handler) error {
	p := sockPath(service, runDir)
	_ = os.Remove(p)
	ln, err := net.Listen("unix", p)
	if err != nil {
		return fmt.Errorf("bind stream socket %s: %w", p, err)
	}
	if err := os.Chmod(p, 0o660); err != nil {
		return fmt.Errorf("chmod stream socket: %w", err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return srv.Serve(ln)
}

// Client returns an http.Client that dials the service's stream socket regardless of the URL host ,
// callers use "http://ghost/<path>" and the host is ignored. NO overall timeout: streams run for
// minutes by design; cancellation comes from the request context.
func Client(service, runDir string) *http.Client {
	p := sockPath(service, runDir)
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 3 * time.Second}
				return d.DialContext(ctx, "unix", p)
			},
		},
	}
}
