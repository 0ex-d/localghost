package oracled

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The projector is looked for at every start: switched off by hand it says how to put it back,
// missing it says where it should be, and put back it is used again without oracled restarting.
func TestProjectorIsLookedForAtEveryStart(t *testing.T) {
	dir := t.TempDir()
	mm := filepath.Join(dir, "mmproj-F16.gguf")
	b := NewLlamaBackend(LlamaConfig{MmprojPath: mm})

	if p, why := b.projector(); p != "" || !strings.Contains(why, "no projector at "+mm) {
		t.Fatalf("missing: %q %q", p, why)
	}
	if err := os.WriteFile(mm+".off", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, why := b.projector(); p != "" || !strings.Contains(why, "switched off by hand (mmproj-F16.gguf.off)") || !strings.Contains(why, "rename it back to mmproj-F16.gguf") {
		t.Fatalf("switched off: %q %q", p, why)
	}
	if err := os.Rename(mm+".off", mm); err != nil {
		t.Fatal(err)
	}
	if p, why := b.projector(); p != mm || why != "" {
		t.Fatalf("put back: %q %q", p, why)
	}
	if b.cfg.MmprojPath != mm {
		t.Fatal("the configured path was forgotten")
	}
	if p, why := NewLlamaBackend(LlamaConfig{}).projector(); p != "" || !strings.Contains(why, "mmprojPath is empty") {
		t.Fatalf("not configured: %q %q", p, why)
	}
	b.setVision(false, "why")
	if on, why := b.Vision(); on || why != "why" {
		t.Fatalf("vision %v %q", on, why)
	}
}
