package secd

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"strings"
)

// handleModels serves the catalogue the app's availableModels expects: id, name, detail, sizeBytes,
// sha256. It reads the unencrypted shared model area, so it needs no mounted account.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	list, err := s.models.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "model catalogue unavailable")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, m := range list {
		out = append(out, map[string]any{
			"id": m.ID, "name": m.Name, "detail": m.Detail,
			"sizeBytes": m.SizeBytes, "sha256": m.SHA256,
		})
	}
	writeJSON(w, map[string]any{"models": out})
}

// handleModelBytes streams a model's bytes for the phone to download and run locally. Supports the
// Range header so a download can resume. /v1/models/{id}
func (s *Server) handleModelBytes(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "model id required")
		return
	}
	rc, size, err := s.models.Open(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "model not found")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Accept-Ranges", "bytes")
	// http.ServeContent would be ideal but needs a ReadSeeker; Open returns one (a file), so:
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, id, modTimeZero, rs)
		return
	}
	w.Header().Set("Content-Length", itoa(size))
	_, _ = io.Copy(w, rc)
}

// --- helpers shared across handlers ---

func clientID(r *http.Request) string {
	// Behind nginx the device is identified by its client cert; nginx passes it as a header. Fall
	// back to RemoteAddr for the rate-limit bucket when running without nginx (local testing).
	if c := r.Header.Get("X-Client-Cert"); c != "" {
		return c
	}
	return r.RemoteAddr
}

func newDeviceToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
