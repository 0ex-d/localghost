package search

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A copy of this test binary started with EMBED_TEST_SERVE answers /health on the --port it is
// given: a stand-in for the embeddings llama-server.
func init() {
	if os.Getenv("EMBED_TEST_SERVE") == "" {
		return
	}
	port := ""
	for i, a := range os.Args {
		if a == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	_ = http.ListenAndServe("127.0.0.1:"+port, nil)
	os.Exit(0)
}

// The embedder killed from outside comes back by itself; Stop ends it for good.
func TestEmbedServerComesBack(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	src, err := os.ReadFile(self)
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "llama-server")
	model := filepath.Join(dir, "emb.gguf")
	if os.WriteFile(bin, src, 0o755) != nil || os.WriteFile(model, []byte("x"), 0o600) != nil {
		t.Fatal("setup")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	t.Setenv("EMBED_TEST_SERVE", "1")
	old := embedBackoff
	embedBackoff = []time.Duration{50 * time.Millisecond}
	defer func() { embedBackoff = old }()

	e := NewEmbedServer(EmbedServerConfig{BinPath: bin, ModelPath: model, Port: port})
	if err := e.Start(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var said []string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watched := make(chan struct{})
	go func() {
		e.Watch(ctx, func(msg string, args ...any) { mu.Lock(); said = append(said, msg); mu.Unlock() })
		close(watched)
	}()
	first := e.cmd.Process.Pid
	_ = syscall.Kill(first, syscall.SIGKILL)
	healthy := func() bool {
		r, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode == 200
	}
	deadline := time.Now().Add(15 * time.Second)
	back := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(strings.Join(said, "|"), "embedding server back")
	}
	for {
		e.mu.Lock()
		c := e.cmd
		e.mu.Unlock()
		if back() && c != nil && c.Process.Pid != first && healthy() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the embedder did not come back")
		}
		time.Sleep(50 * time.Millisecond)
	}
	e.Stop()
	select {
	case <-watched:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not end with Stop")
	}
	if healthy() {
		t.Fatal("still serving after Stop")
	}
}
