package oracled

// Images the engine died on. A photo that makes llama-server abort would otherwise do it again on
// every retry of its caption job (five tries, five crashes, chat down for each restart). oracled
// counts a strike against an image when the engine dies while that image is in flight; at two
// strikes the image is refused before it is sent, and its job parks with the reason. Two, not one:
// the engine can die for a reason that is not the image (the GPU, a signal), and one innocent photo
// should not be blamed for that. The count lives beside the conf, on the volume, so a restart of
// oracled does not forget. Deleting the file forgives every image.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// strikesToRefuse: this many engine deaths with the same image in flight and it is not sent again.
const strikesToRefuse = 2

type imageStrikes struct {
	mu   sync.Mutex
	n    map[string]int
	path string
}

// newImageStrikes loads the counts from path ("<sha256 hex> <count>" per line); an empty path keeps
// them in memory only.
func newImageStrikes(path string) *imageStrikes {
	s := &imageStrikes{n: map[string]int{}, path: path}
	if path == "" {
		return s
	}
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) != 2 || len(parts[0]) != 64 {
			continue
		}
		if c, err := strconv.Atoi(parts[1]); err == nil && c > 0 {
			s.n[parts[0]] = c
		}
	}
	return s
}

// imageKey names an image by what is sent (its data URI), so the same photo is the same key
// whatever path or job it comes from.
func imageKey(uri string) string {
	h := sha256.Sum256([]byte(uri))
	return hex.EncodeToString(h[:])
}

func (s *imageStrikes) refused(key string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n[key] >= strikesToRefuse
}

// strike counts one engine death against key and returns its count.
func (s *imageStrikes) strike(key string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n[key]++
	c := s.n[key]
	if s.path != "" {
		keys := make([]string, 0, len(s.n))
		for k := range s.n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s %d\n", k, s.n[k])
		}
		if err := os.WriteFile(s.path+".tmp", []byte(b.String()), 0o640); err == nil {
			_ = os.Rename(s.path+".tmp", s.path)
		}
	}
	return c
}

// diedDuring reports whether the engine ended during a request that just failed: its exit is seen
// within a few seconds (the reaper closes exited as soon as the process is gone).
func (b *llamaBackend) diedDuring(within time.Duration) bool {
	ex := b.exited
	if ex == nil {
		return false
	}
	select {
	case <-ex:
		return true
	case <-time.After(within):
		return false
	}
}
