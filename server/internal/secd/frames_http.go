package secd

// Photo and location intake. secd stays THIN here on purpose: it authenticates the session, streams
// bytes to ghost.framed's intake folder, and never decodes an image or parses a coordinate. All
// interpretation happens in ghost.framed, on the volume, behind the front door. That keeps secd (the
// one root, network-facing component) free of image parsers , historically one of the most
// exploit-rich code families you can put in front of untrusted input , and keeps the linear trust
// story: the network reaches exactly one small program, and that program only moves bytes.
//
// Write protocol shared with framed: stream to <name>.part, fsync, rename. framed skips *.part, so a
// half-written upload is never processed; the rename is the commit.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// uploadMaxBytes bounds one photo upload. 64MB fits any phone photo with headroom; anything bigger is
// either not a photo or not our problem.
const uploadMaxBytes = 64 << 20

// locationsMaxBytes bounds one location batch. A day of 1Hz samples is ~5MB of JSON; 16MB is generous.
const locationsMaxBytes = 16 << 20

// handleFrameUpload accepts one raw image per POST and spools it for ghost.framed. The body is the
// image bytes, exactly as shot , no multipart, no re-encode, no inspection. framed archives the same
// bytes it receives here.
func (s *Server) handleFrameUpload(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) {
		s.appearsDown(w)
		return
	}
	if r.Method != http.MethodPost {
		s.appearsDown(w)
		return
	}
	s.mu.Lock()
	mounted := s.mounted
	s.mu.Unlock()
	if mounted < 0 {
		s.appearsDown(w) // locked box takes nothing; the app queues and retries after unlock
		return
	}
	dir := filepath.Join(s.cfg.StateDir, "mnt", fmt.Sprintf("slot%d", mounted), "frames", "incoming")
	if err := spoolBody(dir, r, uploadMaxBytes); err != nil {
		secdLog.Warn("frame upload spool failed", "fn", "handleFrameUpload", "err", err)
		http.Error(w, "upload failed", http.StatusInsufficientStorage)
		return
	}
	w.WriteHeader(http.StatusAccepted) // accepted for processing; framed does the rest asynchronously
}

// handleLocations accepts a JSON batch of location points ({"source":..,"points":[{ts,lat,lon}..]})
// and spools it. secd does not parse it; framed validates and drops malformed batches.
func (s *Server) handleLocations(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) {
		s.appearsDown(w)
		return
	}
	if r.Method != http.MethodPost {
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
	dir := filepath.Join(s.cfg.StateDir, "mnt", fmt.Sprintf("slot%d", mounted), "frames", "incoming-locations")
	if err := spoolBody(dir, r, locationsMaxBytes); err != nil {
		secdLog.Warn("location spool failed", "fn", "handleLocations", "err", err)
		http.Error(w, "upload failed", http.StatusInsufficientStorage)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// spoolBody streams the request body to a fresh .part file in dir, fsyncs, and renames it live. The
// name is arrival-ordered (nanosecond timestamp) plus random hex so concurrent uploads never collide.
func spoolBody(dir string, r *http.Request, maxBytes int64) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	var rb [6]byte
	_, _ = rand.Read(rb[:])
	name := fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(rb[:]))
	part := filepath.Join(dir, name+".part")
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	body := http.MaxBytesReader(nil, r.Body, maxBytes)
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		_ = os.Remove(part)
		return err
	}
	if err := f.Sync(); err != nil { // the bytes are the photo; make sure they hit the disk
		_ = f.Close()
		_ = os.Remove(part)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, filepath.Join(dir, name)) // the commit: framed only sees completed files
}
