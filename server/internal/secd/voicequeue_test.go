package secd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/voiced"
)

func TestVoiceQueue(t *testing.T) {
	mount := t.TempDir()
	now := time.Now()
	if q := voiceQueue(mount, now); q["running"] != false || q["why"] != nil {
		t.Fatalf("no state file: %v", q)
	}
	_ = os.MkdirAll(filepath.Join(mount, "voiced"), 0o750)
	write := func(st voiced.State) {
		b, _ := json.Marshal(st)
		if err := os.WriteFile(voiced.StatePath(mount), b, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	write(voiced.State{Why: "no speech engine", At: now.Add(-10 * time.Second).UnixMilli()})
	if q := voiceQueue(mount, now); q["running"] != true || q["why"] != "no speech engine" {
		t.Fatalf("fresh: %v", q)
	}
	write(voiced.State{Why: "no speech engine", At: now.Add(-5 * time.Minute).UnixMilli()})
	if q := voiceQueue(mount, now); q["running"] != false || q["why"] != nil {
		t.Fatalf("stale: %v", q)
	}
}

func TestCleanHashes(t *testing.T) {
	good := "0123456789abcdef0123456789abcdef"
	got := cleanHashes([]string{good, "0123456789ABCDEF0123456789ABCDEF", "short", good + "00", "0123456789abcdef0123456789abcde'", ""})
	if len(got) != 1 || got[0] != good {
		t.Fatalf("%v", got)
	}
}
