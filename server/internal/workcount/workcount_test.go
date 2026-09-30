package workcount

import (
	"testing"
	"time"
)

func TestCountsByHourDayAndSinceStart(t *testing.T) {
	t0 := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	now := t0
	c := newAt(func() time.Time { return now })
	c.Add("photos", 3)
	now = t0.Add(30 * time.Minute)
	c.Add("photos", 2)
	c.Add("videos", 1)
	now = t0.Add(80 * time.Minute) // the first three are now over an hour old
	c.Add("photos", 1)
	s := c.Snapshot()
	if s.Since != t0.Unix() || len(s.Kinds) != 2 {
		t.Fatalf("snapshot %+v", s)
	}
	p := s.Kinds[0]
	if p.Kind != "photos" || p.Hour != 3 || p.Day != 6 || p.Total != 6 || p.LastAt != now.Unix() {
		t.Fatalf("photos %+v", p)
	}
	// a day and more later, the day count forgets, the total does not
	now = t0.Add(26 * time.Hour)
	c.Add("photos", 4)
	p = c.Snapshot().Kinds[0]
	if p.Hour != 4 || p.Day != 4 || p.Total != 10 {
		t.Fatalf("a day on %+v", p)
	}
	// a nil counter counts nothing and says nothing
	var nc *Counter
	nc.Add("x", 1)
	if s := nc.Snapshot(); len(s.Kinds) != 0 {
		t.Fatal("nil counter")
	}
}
