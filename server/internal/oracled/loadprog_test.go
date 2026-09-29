package oracled

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }
func feed(w *EngineWatch, s string)      { _, _ = w.Write([]byte(s)) }
func newTestWatch(path string) (*EngineWatch, *engineInfoBox, *fakeClock) {
	box := newEngineInfoBox()
	box.load = newLoadTracker(path)
	c := &fakeClock{t: time.Unix(1_790_000_000, 0)}
	box.load.now = c.now
	return box.watcher(nil), box, c
}

// A load as llama-server prints it: the loader's lines, the dots one per percent on a line of
// their own, the projector, the warm-up. The phase follows the lines, the weights percent is the
// dot count, and the time left comes from the rate of the dots.
func TestLoadProgressFollowsTheDots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "load.json")
	w, box, c := newTestWatch(path)
	box.load.begin(1)
	if p := box.load.snapshot(); p.Phase != "starting" || p.EtaMs != -1 || p.Pct != 3 {
		t.Fatalf("start: %+v", p)
	}
	c.add(time.Second)
	feed(w, "llama_model_loader: loaded meta data with 44 key-value pairs\n")
	feed(w, "load_tensors: offloaded 49/49 layers to GPU\nload_tensors: loading model tensors, this can take a while... (mmap = true)\n")
	if p := box.load.snapshot(); p.Phase != "weights" {
		t.Fatalf("weights: %+v", p)
	}
	// 25 dots in 5 s: 15 s to go for the weights, plus 3 s with no history
	c.add(5 * time.Second)
	feed(w, strings.Repeat(".", 25))
	p := box.load.snapshot()
	if p.WeightsPct != 25 || p.EtaMs != 18000 {
		t.Fatalf("25%%: %+v", p)
	}
	c.add(15 * time.Second)
	feed(w, strings.Repeat(".", 75)+"\n")
	if p := box.load.snapshot(); p.Phase != "finishing" || p.WeightsPct != 100 {
		t.Fatalf("after the dots: %+v", p)
	}
	feed(w, "clip_model_loader: model name: gemma-4\n")
	if p := box.load.snapshot(); p.Phase != "projector" {
		t.Fatalf("projector: %+v", p)
	}
	c.add(2 * time.Second)
	feed(w, "common_init_from_params: warming up the model with an empty run - please wait ...\n")
	if p := box.load.snapshot(); p.Phase != "warmup" || p.Pct < 90 {
		t.Fatalf("warmup: %+v", p)
	}
	c.add(time.Second)
	box.load.ready()
	if p := box.load.snapshot(); p.Phase != "ready" || p.Pct != 100 || p.EtaMs != 0 {
		t.Fatalf("ready: %+v", p)
	}
	// the times are kept, and the next start estimates from them
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), `"totalMs":24000`) || !strings.Contains(string(b), `"postMs":3000`) {
		t.Fatalf("kept times: %s %v", b, err)
	}
	w2, box2, c2 := newTestWatch(path)
	box2.load.begin(1)
	c2.add(4 * time.Second)
	if p := box2.load.snapshot(); p.EtaMs != 20000 || p.LastMs != 24000 || p.Pct != 16 {
		t.Fatalf("second load, from history: %+v", p)
	}
	feed(w2, "load_tensors: loading model tensors\n")
	c2.add(10 * time.Second)
	feed(w2, strings.Repeat(".", 50))
	// 50 dots in 10 s: 10 s to go, plus last time's 3 s after the weights
	if p := box2.load.snapshot(); p.EtaMs != 13000 {
		t.Fatalf("second load, weights: %+v", p)
	}
}

// The percent never goes backwards, even when the estimate grows (a slow stretch of the disk).
func TestLoadProgressNeverGoesBack(t *testing.T) {
	w, box, c := newTestWatch("")
	box.load.begin(1)
	feed(w, "load_tensors: loading\n")
	c.add(2 * time.Second)
	feed(w, strings.Repeat(".", 40))
	first := box.load.snapshot().Pct
	c.add(20 * time.Second) // nothing new: the rate falls, the estimate grows
	if p := box.load.snapshot(); p.Pct < first {
		t.Fatalf("went back from %d to %d", first, p.Pct)
	}
	box.load.fail()
	if p := box.load.snapshot(); p.Phase != "failed" || p.EtaMs != -1 {
		t.Fatalf("failed: %+v", p)
	}
}
