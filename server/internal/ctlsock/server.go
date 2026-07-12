// Package ctlsock is the shared control socket every ghost.*d daemon exposes, the way redis-cli talks
// to redis. Each daemon listens on a unix socket at <mount>/run/<service>.sock (0600, on the encrypted
// volume, so filesystem perms are the auth and it is crypto-erased on lock). A client , ghost-cli or
// another daemon like watchd , connects and sends one JSON command per connection.
//
// Every daemon gets a BASE command set for free: ping, reload (re-read <service>.conf), status,
// log-level. A daemon adds its OWN commands (ghost.cued: queue; ghost.oracled: models) by registering
// handlers. The command set a caller sees therefore depends on which service it connected to , exactly
// the redis-cli-per-service model.
package ctlsock

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// Request is one command. Args is a free-form bag so each service defines its own command shapes
// without a shared schema; a handler decodes the args it expects.
type Request struct {
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response is the reply. Data is the handler's payload (any JSON), OK/Err carry success. Text is an
// optional human line ghost-cli prints as-is, so a handler can return a pre-formatted table.
type Response struct {
	OK   bool            `json:"ok"`
	Err  string          `json:"err,omitempty"`
	Text string          `json:"text,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Handler runs one command. It returns a Response; a returned error is folded into Response.Err by the
// server, so a handler can just return an error for the failure path.
type Handler func(args json.RawMessage) (Response, error)

// Server is a daemon's control socket. Build it, register per-service handlers, then Serve.
type Server struct {
	service  string
	sockPath string
	log      *slog.Logger
	mu       sync.RWMutex
	handlers map[string]Handler
	ln       net.Listener
}

// NewServer prepares a control server for service at <runDir>/<service>.sock. runDir is <mount>/run.
func NewServer(service, runDir string, log *slog.Logger) *Server {
	return &Server{
		service:  service,
		sockPath: filepath.Join(runDir, service+".sock"),
		log:      log,
		handlers: map[string]Handler{},
	}
}

// Handle registers a command. Base commands (ping/status/reload/log-level) are added by the daemon via
// this same call, so a service can override them if it must, but usually just adds its own on top.
func (s *Server) Handle(cmd string, h Handler) {
	s.mu.Lock()
	s.handlers[cmd] = h
	s.mu.Unlock()
}

// Commands lists the registered command names (for a "help"/"commands" base handler).
func (s *Server) Commands() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.handlers))
	for c := range s.handlers {
		out = append(out, c)
	}
	return out
}

// Serve binds the socket and dispatches until ctx is cancelled. A stale socket from an unclean exit is
// removed first (safe: if a live daemon were bound, this one would not have been started).
func (s *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.sockPath), 0o750); err != nil {
		return err
	}
	_ = os.Remove(s.sockPath)
	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.sockPath, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	s.ln = ln

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.log.Error("control accept", "fn", "Serve", "err", err)
			continue
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Err: "bad request: " + err.Error()})
		return
	}
	s.mu.RLock()
	h, ok := s.handlers[req.Cmd]
	s.mu.RUnlock()
	if !ok {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Err: "unknown command: " + req.Cmd})
		return
	}
	resp, err := h(req.Args)
	if err != nil {
		// Log in THIS service's own log, not only on the wire back to the caller. Before this, a
		// daemon's command failure (e.g. synthd's chat forward to oracled timing out) appeared only
		// in the CALLER's log , the daemon that actually failed stayed silent about it, and its log
		// read as if nothing ever went wrong. Every ctlsock service gets this line for free.
		s.log.Warn("command failed", "fn", "handle", "cmd", req.Cmd, "err", err)
		resp = Response{OK: false, Err: err.Error()}
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// Cleanup closes the listener and removes the socket file.
func (s *Server) Cleanup() {
	if s.ln != nil {
		_ = s.ln.Close()
	}
	_ = os.Remove(s.sockPath)
}
