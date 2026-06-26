package secd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/LocalGhostDao/localghost/server/internal/models"
	"github.com/LocalGhostDao/localghost/server/internal/profile"
)

// Server is the ghost.secd HTTP surface the phone talks to. It wires the library packages into the
// handlers the app's BoxClient calls: enroll, unlock (streamed), info, and the model catalogue.
//
// Auth model recap, enforced by the layers around this: nginx terminates TLS and rejects any client
// without a box-issued device cert at the handshake (the access key), so every request that reaches
// here is already from an enrolled device. The PIN (account selection) is then proven at /unlock.
type Server struct {
	cfg      Config
	models   *models.Registry
	mu       sync.Mutex
	mounted  int // currently mounted slot, -1 if locked
	enroll   *enrollService
	unlock   *unlockService
}

type Config struct {
	StateDir string // unencrypted: /var/lib/ghost (certs, models, enrollment records)
}

func New(cfg Config) (*Server, error) {
	if cfg.StateDir == "" {
		cfg.StateDir = "/var/lib/ghost"
	}
	if err := os.MkdirAll(filepath.Join(cfg.StateDir, "models"), 0o755); err != nil {
		return nil, fmt.Errorf("state dir: %w", err)
	}
	s := &Server{
		cfg:     cfg,
		models:  models.NewRegistry(filepath.Join(cfg.StateDir, "models")),
		mounted: -1,
	}
	s.enroll = newEnrollService(cfg.StateDir)
	// newDefaultBackend is build-tag-selected: the simulation in the default build, the real TPM +
	// dm-crypt + Postgres/Redis backend with -tags tpm. This is the seam where unlock meets hardware.
	s.unlock = newUnlockService(newDefaultBackend(cfg))
	return s, nil
}

// Handler returns the routed mux. Routes match what the app's BoxClient calls.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/enroll", s.handleEnroll)
	mux.HandleFunc("/v1/unlock", s.handleUnlockStart)
	mux.HandleFunc("/v1/unlock/poll", s.handleUnlockPoll)
	mux.HandleFunc("/v1/info", s.handleInfo)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/models/", s.handleModelBytes) // /v1/models/{id}
	return logRequests(mux)
}

// handleHealth is the cheap reachability check the app's reachable() calls. It needs no account.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "service": "ghost.secd"})
}

// handleInfo returns box + mounted-account summary for the app's home screen. Returns locked state
// if no account is mounted.
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	mounted := s.mounted
	s.mu.Unlock()
	if mounted < 0 {
		writeJSON(w, map[string]any{"locked": true})
		return
	}
	writeJSON(w, map[string]any{
		"locked":      false,
		"mountedSlot": mounted,
		"daemons":     profile.TotalSlots, // placeholder until the backing daemons report in
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	writeJSON(w, map[string]any{"error": msg})
}

// SetDeviceIssuer wires the box PKI so enrolment can mint device certs. Called by the daemon once
// the CA exists.
func (s *Server) SetDeviceIssuer(i DeviceIssuer) { s.enroll.SetIssuer(i) }

// ArmEnrollment sets the one-time pairing code that the QR carries, enabling enrolment. Setup calls
// this (via the daemon) after issuing the QR; it is cleared after one successful enrol.
func (s *Server) ArmEnrollment(pairingCode string) { s.enroll.SetPairingCode(pairingCode) }
