package oracled

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A llama-server that dies at start (here: a flag it does not know, the way a build from another
// commit answers oracled's arguments) is noticed at once, with its own words, not after minutes of
// polling a dead port with "not healthy" as the only explanation.
func TestStartReportsAChildThatDies(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "llama-server")
	script := "#!/bin/sh\necho 'build: 7fe450e (cuda)'\necho 'error: invalid argument: --no-webui' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(dir, "m.gguf")
	_ = os.WriteFile(model, make([]byte, 2<<20), 0o644)
	b := NewLlamaBackend(LlamaConfig{BinPath: bin, ModelPath: model, Port: 18999, ModelName: "test"})
	t0 := time.Now()
	err := b.Start(context.Background())
	if err == nil {
		t.Fatal("a child that exited was taken as started")
	}
	if time.Since(t0) > 10*time.Second {
		t.Fatalf("took %s to notice", time.Since(t0))
	}
	if !strings.Contains(err.Error(), "invalid argument: --no-webui") || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("the reason is not in the error: %v", err)
	}
	b.Stop() // already gone: returns at once
	// and a second start tells its own story, not the first one's lines again
	script2 := "#!/bin/sh\necho 'ggml_cuda_init: found 1 CUDA devices'\necho 'CUDA error: out of memory' >&2\nexit 2\n"
	_ = os.WriteFile(bin, []byte(script2), 0o755)
	err = b.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "out of memory") || strings.Contains(err.Error(), "--no-webui") {
		t.Fatalf("second start: %v", err)
	}
}

// A flag from conf that this llama.cpp no longer knows is dropped (with its value when it has
// one), and the start goes on without it; oracled's own arguments are never touched.
func TestDropRejectedArg(t *testing.T) {
	b := NewLlamaBackend(LlamaConfig{ExtraArgs: []string{"--threads", "4", "-c", "65536", "--flash-attn", "on", "--mlock"}})
	err := errors.New(`llama-server exited (exit status 1) before it was ready; it said: error: invalid argument: --mlock`)
	if d, ok := b.DropRejectedArg(err); !ok || d != "--mlock" {
		t.Fatalf("dropped %q %v", d, ok)
	}
	if got := strings.Join(b.cfg.ExtraArgs, " "); got != "--threads 4 -c 65536 --flash-attn on" {
		t.Fatalf("left %q", got)
	}
	if d, ok := b.DropRejectedArg(errors.New("error: invalid argument: --flash-attn")); !ok || d != "--flash-attn on" {
		t.Fatalf("with its value: %q %v", d, ok)
	}
	if _, ok := b.DropRejectedArg(errors.New("error: invalid argument: --no-webui")); ok {
		t.Fatal("dropped an argument that was not from conf")
	}
	if _, ok := b.DropRejectedArg(errors.New("CUDA error: out of memory")); ok {
		t.Fatal("dropped something on an error that names no argument")
	}
}
