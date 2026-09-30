package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteListensMovesPlain443(t *testing.T) {
	in := `server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name example.com;
}
server {
    listen 80;
    server_name plain.example.com;
}
`
	out, n, ipv6, err := RewriteListens(in, "127.0.0.1:4443")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || !ipv6 {
		t.Fatalf("moved %d ipv6=%v, want 2 and true", n, ipv6)
	}
	if !strings.Contains(out, "listen 127.0.0.1:4443 ssl proxy_protocol;") {
		t.Fatalf("v4 not moved with proxy_protocol:\n%s", out)
	}
	if !strings.Contains(out, "# localghost moved (the stream listens on [::]:443)") {
		t.Fatalf("v6 not commented (its block already has a moved v4 line):\n%s", out)
	}
	if !strings.Contains(out, "listen 80;") {
		t.Fatal("port 80 must be left alone")
	}
}

// A block that listens only on [::]:443 has that line moved, not commented (nothing else carries it).
func TestRewriteListensIPv6Only(t *testing.T) {
	in := "server {\n    listen [::]:443 ssl;\n    server_name six.example.com;\n}\n"
	out, n, ipv6, err := RewriteListens(in, "127.0.0.1:4443")
	if err != nil || n != 1 || !ipv6 {
		t.Fatalf("n=%d ipv6=%v err=%v", n, ipv6, err)
	}
	if !strings.Contains(out, "listen 127.0.0.1:4443 ssl proxy_protocol;") {
		t.Fatalf("the sole v6 listener should be moved:\n%s", out)
	}
}

func TestRewriteListensRefusesASpecificAddress(t *testing.T) {
	in := "server {\n    listen 203.0.113.5:443 ssl;\n}\n"
	if _, _, _, err := RewriteListens(in, "127.0.0.1:4443"); err == nil {
		t.Fatal("a listener bound to a specific public address must be refused, not guessed")
	}
}

func TestRewriteListensLeavesQuic(t *testing.T) {
	in := "server {\n    listen 443 quic reuseport;\n    listen 443 ssl;\n}\n"
	out, n, _, err := RewriteListens(in, "127.0.0.1:4443")
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v (only the ssl line moves; quic is UDP)", n, err)
	}
	if !strings.Contains(out, "listen 443 quic reuseport;") {
		t.Fatal("the quic listener must be left alone")
	}
}

func TestAddStreamIncludeNewBlock(t *testing.T) {
	out := AddStreamInclude("http {\n}\n", StreamConfPath)
	if !strings.Contains(out, "stream {") || !strings.Contains(out, "include "+StreamConfPath+";") {
		t.Fatalf("a stream block with the include was not added:\n%s", out)
	}
	// idempotent
	if AddStreamInclude(out, StreamConfPath) != out {
		t.Fatal("adding the include twice changed the file")
	}
}

func TestAddStreamIncludeExistingBlock(t *testing.T) {
	in := "stream {\n    upstream x { server 127.0.0.1:9000; }\n}\n"
	out := AddStreamInclude(in, StreamConfPath)
	if strings.Count(out, "stream {") != 1 {
		t.Fatalf("a second stream block was added:\n%s", out)
	}
	if !strings.Contains(out, "include "+StreamConfPath+";") {
		t.Fatal("the include was not put in the existing stream block")
	}
}

func TestStreamConfRoutesByName(t *testing.T) {
	p := Passthrough{Domain: "box.example.com", SecdAddr: "127.0.0.1:8443", SitesAddr: "127.0.0.1:4443"}
	out := p.StreamConf(true, true)
	for _, want := range []string{"box.example.com  127.0.0.1:8443;", "default  127.0.0.1:4443;",
		"ssl_preread on;", "proxy_protocol on;", "listen [::]:443;"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stream conf missing %q:\n%s", want, out)
		}
	}
	// a box that serves nothing else: unknown names go to secd too, so they meet the same 503
	solo := p.StreamConf(false, false)
	if !strings.Contains(solo, "default  127.0.0.1:8443;") {
		t.Fatalf("solo box should default to secd:\n%s", solo)
	}
}

// fakeNginx records whether test/reload/check ran and can be told to fail at a chosen step.
type fakeNginx struct {
	failTest, failCheck bool
	tested, reloaded    int
}

func (f *fakeNginx) TestNginx() error {
	f.tested++
	if f.failTest {
		return os.ErrInvalid
	}
	return nil
}
func (f *fakeNginx) ReloadNginx() error { f.reloaded++; return nil }
func (f *fakeNginx) Check(Passthrough, string) error {
	if f.failCheck {
		return os.ErrDeadlineExceeded
	}
	return nil
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestApplyPassthroughEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"etc/nginx/nginx.conf":               "http {\n    include /etc/nginx/conf.d/*.conf;\n    include /etc/nginx/sites-enabled/*;\n}\n",
		"etc/nginx/sites-enabled/ghost-secd": "server { listen 443 ssl; server_name box.example.com; }\n",
		"etc/nginx/sites-enabled/blog":       "server {\n    listen 443 ssl;\n    server_name blog.example.com;\n}\n",
	})
	p := Passthrough{Domain: "box.example.com", SecdAddr: "127.0.0.1:8443", SitesAddr: "127.0.0.1:4443"}
	f := &fakeNginx{}
	res, err := ApplyPassthrough(root, p, f, false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if f.tested == 0 || f.reloaded == 0 {
		t.Fatal("nginx was not tested and reloaded")
	}
	if res.OtherName != "blog.example.com" {
		t.Fatalf("the check should use the other site's name, got %q", res.OtherName)
	}
	// the blog moved to :4443 with proxy_protocol; the ghost site link is gone
	blog, _ := os.ReadFile(filepath.Join(root, "etc/nginx/sites-enabled/blog"))
	if !strings.Contains(string(blog), "127.0.0.1:4443") {
		t.Fatalf("the blog site was not moved:\n%s", blog)
	}
	if _, err := os.Lstat(filepath.Join(root, GhostSiteLink)); err == nil {
		t.Fatal("the old ghost-secd http site should be disabled")
	}
	nc, _ := os.ReadFile(filepath.Join(root, "etc/nginx/nginx.conf"))
	if !strings.Contains(string(nc), "include "+StreamConfPath+";") {
		t.Fatal("the stream include was not added to nginx.conf")
	}
	if _, err := os.Stat(filepath.Join(root, StreamConfPath)); err != nil {
		t.Fatal("the stream conf was not written")
	}
}

func TestApplyPassthroughRollsBackOnCheckFailure(t *testing.T) {
	root := t.TempDir()
	orig := "server {\n    listen 443 ssl;\n    server_name blog.example.com;\n}\n"
	writeTree(t, root, map[string]string{
		"etc/nginx/nginx.conf":         "http {\n    include /etc/nginx/sites-enabled/*;\n}\n",
		"etc/nginx/sites-enabled/blog": orig,
	})
	p := Passthrough{Domain: "box.example.com", SecdAddr: "127.0.0.1:8443", SitesAddr: "127.0.0.1:4443"}
	f := &fakeNginx{failCheck: true}
	_, err := ApplyPassthrough(root, p, f, false)
	if err == nil {
		t.Fatal("a failing check must fail the apply")
	}
	// everything put back: the blog is its original self, the stream conf gone, nginx.conf clean
	blog, _ := os.ReadFile(filepath.Join(root, "etc/nginx/sites-enabled/blog"))
	if string(blog) != orig {
		t.Fatalf("the blog was not restored:\n%s", blog)
	}
	if _, err := os.Stat(filepath.Join(root, StreamConfPath)); err == nil {
		t.Fatal("the stream conf should have been removed on rollback")
	}
	nc, _ := os.ReadFile(filepath.Join(root, "etc/nginx/nginx.conf"))
	if strings.Contains(string(nc), StreamConfPath) {
		t.Fatal("nginx.conf still has the stream include after rollback")
	}
	if f.reloaded < 2 {
		t.Fatal("nginx should have been reloaded onto the restored config")
	}
}

func TestUndoPassthrough(t *testing.T) {
	root := t.TempDir()
	orig := "server {\n    listen 443 ssl;\n    server_name blog.example.com;\n}\n"
	writeTree(t, root, map[string]string{
		"etc/nginx/nginx.conf":         "http {\n    include /etc/nginx/sites-enabled/*;\n}\n",
		"etc/nginx/sites-enabled/blog": orig,
	})
	p := Passthrough{Domain: "box.example.com", SecdAddr: "127.0.0.1:8443", SitesAddr: "127.0.0.1:4443"}
	f := &fakeNginx{}
	if _, err := ApplyPassthrough(root, p, f, false); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoPassthrough(root, "", f); err != nil {
		t.Fatalf("undo: %v", err)
	}
	blog, _ := os.ReadFile(filepath.Join(root, "etc/nginx/sites-enabled/blog"))
	if string(blog) != orig {
		t.Fatalf("undo did not restore the blog:\n%s", blog)
	}
	if _, err := os.Stat(filepath.Join(root, StreamConfPath)); err == nil {
		t.Fatal("undo left the stream conf behind")
	}
}
