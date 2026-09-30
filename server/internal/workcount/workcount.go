// Package workcount counts what a daemon did, by kind, over the last hour and the last day, in
// one-minute buckets, and since the daemon started. The Box Status drill-ins ask for it: the
// database says how far the archive is, and only the daemon knows how much it did today. In memory
// only: a restart (an unlock) starts the count again, and the drill-in says since when.
package workcount

import (
	"sort"
	"sync"
	"time"
)

const minutes = 24 * 60

// Counter is safe for concurrent use. The zero value is not ready: use New.
type Counter struct {
	mu    sync.Mutex
	start time.Time
	now   func() time.Time
	kinds map[string]*series
}

type series struct {
	total  int64
	last   time.Time
	counts [minutes]int64
	stamps [minutes]int64 // which minute (unix/60) each bucket holds
}

// New starts counting now.
func New() *Counter { return newAt(time.Now) }

func newAt(now func() time.Time) *Counter {
	return &Counter{start: now(), now: now, kinds: map[string]*series{}}
}

// Add counts n of kind now. A nil counter counts nothing.
func (c *Counter) Add(kind string, n int) {
	if c == nil || n <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.kinds[kind]
	if s == nil {
		s = &series{}
		c.kinds[kind] = s
	}
	now := c.now()
	m := now.Unix() / 60
	i := m % minutes
	if s.stamps[i] != m {
		s.stamps[i], s.counts[i] = m, 0
	}
	s.counts[i] += int64(n)
	s.total += int64(n)
	s.last = now
}

// Kind is one kind's counts.
type Kind struct {
	Kind   string `json:"kind"`
	Hour   int64  `json:"hour"`
	Day    int64  `json:"day"`
	Total  int64  `json:"total"`
	LastAt int64  `json:"lastAt"` // unix seconds
}

// Snapshot is every kind's counts, and when the count began.
type Snapshot struct {
	Since int64  `json:"since"` // unix seconds
	Kinds []Kind `json:"kinds"`
}

func (c *Counter) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now().Unix() / 60
	out := Snapshot{Since: c.start.Unix()}
	for k, s := range c.kinds {
		kc := Kind{Kind: k, Total: s.total, LastAt: s.last.Unix()}
		for i := 0; i < minutes; i++ {
			age := now - s.stamps[i]
			if age < 0 || age >= minutes || s.counts[i] == 0 {
				continue
			}
			kc.Day += s.counts[i]
			if age < 60 {
				kc.Hour += s.counts[i]
			}
		}
		out.Kinds = append(out.Kinds, kc)
	}
	sort.Slice(out.Kinds, func(i, j int) bool { return out.Kinds[i].Kind < out.Kinds[j].Kind })
	return out
}
