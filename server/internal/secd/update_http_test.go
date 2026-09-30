package secd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/profile"
	"github.com/LocalGhostDao/localghost/server/internal/update"
	"github.com/LocalGhostDao/localghost/server/internal/watchd"
)

func releaseBundle(t *testing.T, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range []struct{ name, body string }{
		{"VERSION", version + "\n"}, {"COMMIT", "abc\n"}, {"CHANGES.txt", "one\n"},
		{"bin/ghost.secd", "\x7fELF secd " + version}, {"bin/ghost.watchd", "\x7fELF watchd"},
		{"bin/ghost.framed", "\x7fELF framed " + version},
	} {
		tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func updateServer(t *testing.T) (*Server, update.Paths, func(method, path string, body []byte) *httptest.ResponseRecorder, chan struct{}) {
	t.Helper()
	s := newTestServer(t)
	root := t.TempDir()
	p := update.Paths{State: filepath.Join(root, "update"), BinDir: filepath.Join(root, "bin"),
		Tools: filepath.Join(root, "tools"), Staging: filepath.Join(root, "staging")}
	os.MkdirAll(p.BinDir, 0o755)
	os.WriteFile(filepath.Join(p.BinDir, "ghost.secd"), []byte("old secd"), 0o755)
	s.upd.paths = &p
	s.mu.Lock()
	s.mounted = 0
	s.mu.Unlock()
	cohort := filepath.Join(s.cfg.StateDir, "mnt", "slot0", "bin")
	os.MkdirAll(cohort, 0o755)
	os.WriteFile(filepath.Join(cohort, "ghost.framed"), []byte("old framed"), 0o755)
	restarted := make(chan struct{}, 4)
	old := restartSelf
	restartSelf = func() { restarted <- struct{}{} }
	t.Cleanup(func() { restartSelf = old })
	var tok string
	do := func(method, path string, body []byte) *httptest.ResponseRecorder {
		if tok == "" || !s.session.Valid(tok) {
			tok, _ = s.session.Issue() // the restart locked the box: a new unlock's session
		}
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		s.Handler().ServeHTTP(rr, req)
		return rr
	}
	return s, p, do, restarted
}

func TestUpdateFromThePhone(t *testing.T) {
	s, p, do, restarted := updateServer(t)
	// the verifier stands in for mirror_fetch.sh: it copies the set's files as the real one would
	var verifiedFrom string
	s.upd.verify = func(_ context.Context, _ update.Paths, incoming, dir string) error {
		verifiedFrom = incoming
		if _, err := os.Stat(filepath.Join(incoming, "MANIFEST.txt.asc")); err != nil {
			return errors.New("no signature")
		}
		os.MkdirAll(dir, 0o700)
		b, _ := os.ReadFile(filepath.Join(incoming, "b1", "server", "localghost-server-0.9.3-linux-amd64.tar.gz"))
		return os.WriteFile(filepath.Join(dir, "localghost-server-0.9.3-linux-amd64.tar.gz"), b, 0o600)
	}
	if rr := do("POST", "/v1/update/file?name=../../etc/passwd", []byte("x")); rr.Code != 400 {
		t.Fatalf("a path outside the set: %d", rr.Code)
	}
	for name, body := range map[string][]byte{
		"MANIFEST.txt": []byte("# LocalGhost Mirror Manifest\n"), "MANIFEST.txt.asc": []byte("sig"),
		"b1/server/localghost-server-0.9.3-linux-amd64.tar.gz": releaseBundle(t, "0.9.3"),
	} {
		if name != "MANIFEST.txt" {
			continue
		}
		if rr := do("POST", "/v1/update/file?name="+name, body); rr.Code != 200 {
			t.Fatalf("upload %s: %d", name, rr.Code)
		}
	}
	for _, f := range []struct {
		name string
		body []byte
	}{{"MANIFEST.txt.asc", []byte("sig")}, {"b1/server/localghost-server-0.9.3-linux-amd64.tar.gz", releaseBundle(t, "0.9.3")}} {
		if rr := do("POST", "/v1/update/file?name="+f.name, f.body); rr.Code != 200 {
			t.Fatalf("upload %s: %d", f.name, rr.Code)
		}
	}
	rr := do("POST", "/v1/update/apply", nil)
	var got struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Why     string `json:"why"`
	}
	json.Unmarshal(rr.Body.Bytes(), &got)
	if !got.OK || got.Version != "0.9.3" || verifiedFrom != filepath.Join(p.State, "incoming") {
		t.Fatalf("apply: %s", rr.Body)
	}
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("secd did not restart onto the release")
	}
	if b, _ := os.ReadFile(filepath.Join(p.BinDir, "ghost.secd")); !strings.Contains(string(b), "0.9.3") {
		t.Fatal("secd not replaced")
	}
	if b, _ := os.ReadFile(filepath.Join(p.Staging, "ghost.framed")); !strings.Contains(string(b), "0.9.3") {
		t.Fatal("the cohort is not staged")
	}
	if tr := update.LoadTrial(p); tr.State != "trial" || tr.Prev != Version {
		t.Fatalf("trial %+v", tr)
	}
	// the phone asks for the earlier build back
	if rr := do("POST", "/v1/update/rollback", nil); !strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Fatalf("rollback: %s", rr.Body)
	}
	<-restarted
	if b, _ := os.ReadFile(filepath.Join(p.BinDir, "ghost.secd")); string(b) != "old secd" {
		t.Fatal("the earlier secd is not back")
	}
	if b, _ := os.ReadFile(filepath.Join(p.Staging, "ghost.framed")); string(b) != "old framed" {
		t.Fatal("the earlier cohort is not staged")
	}
}

func TestAnUnverifiedReleaseIsRefused(t *testing.T) {
	s, p, do, restarted := updateServer(t)
	s.upd.verify = func(context.Context, update.Paths, string, string) error { return errors.New("BAD signature") }
	do("POST", "/v1/update/file?name=MANIFEST.txt", []byte("x"))
	rr := do("POST", "/v1/update/apply", nil)
	if !strings.Contains(rr.Body.String(), `"ok":false`) || !strings.Contains(rr.Body.String(), "BAD signature") {
		t.Fatalf("apply: %s", rr.Body)
	}
	select {
	case <-restarted:
		t.Fatal("restarted on a refused release")
	case <-time.After(300 * time.Millisecond):
	}
	if b, _ := os.ReadFile(filepath.Join(p.BinDir, "ghost.secd")); string(b) != "old secd" {
		t.Fatal("secd touched")
	}
}

func TestTrialRollsBackAFailedFirstUnlock(t *testing.T) {
	s, p, _, restarted := updateServer(t)
	rel, err := update.Unpack(writeTemp(t, releaseBundle(t, "0.9.3")), filepath.Join(p.State, "releases", "0.9.3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := update.Apply(p, rel, filepath.Join(s.cfg.StateDir, "mnt", "slot0", "bin"), "0.9.2"); err != nil {
		t.Fatal(err)
	}
	s.trialOnUnlock(false, profile.StageResolve) // a wrong PIN: nothing
	if update.LoadTrial(p).State != "trial" {
		t.Fatal("a wrong PIN rolled the release back")
	}
	s.trialOnUnlock(false, profile.StageStartDB)
	<-restarted
	if tr := update.LoadTrial(p); tr.State != "rolled_back" || !strings.Contains(tr.Reason, "starting database") {
		t.Fatalf("trial %+v", tr)
	}
}

func TestUnhealthyDaemons(t *testing.T) {
	if why := unhealthy([]watchd.ServiceStatus{{Name: "ghost.framed", Critical: true, State: "up"}, {Name: "ghost.cued", State: "down"}}); why != "" {
		t.Fatalf("healthy called unhealthy: %s", why)
	}
	if why := unhealthy([]watchd.ServiceStatus{{Name: "ghost.searchd", Critical: true, State: "up", Restarts: 4}}); !strings.Contains(why, "restarted 4") {
		t.Fatalf("a crash loop not seen: %q", why)
	}
	if why := unhealthy([]watchd.ServiceStatus{{Name: "ghost.framed", Critical: true, State: "backoff"}}); why == "" {
		t.Fatal("a daemon in backoff not seen")
	}
}

func writeTemp(t *testing.T, b []byte) string {
	p := filepath.Join(t.TempDir(), "b.tar.gz")
	os.WriteFile(p, b, 0o600)
	return p
}
