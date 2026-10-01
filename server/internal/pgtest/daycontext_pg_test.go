package pgtest

import (
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// The check-in prefill reads the day's health by the phone's local day, and lists only the
// person's own notes.
func TestDayContextDayAndNotes(t *testing.T) {
	db := fresh(t)
	// Greece, UTC+3: local midnight of 30 Sep is 29 Sep 21:00 UTC
	athens := time.FixedZone("EEST", 3*3600)
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, athens).Unix()
	end := start + 86400
	ins := func(q string, args ...any) {
		t.Helper()
		if err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	ins(`INSERT INTO health_metrics (day, metric, value) VALUES ('2026-09-30','steps',8421), ('2026-09-29','steps',100)`)
	for _, j := range []struct{ src, title string }{
		{"ghost.framed", "photo at Lakka"}, {"ghost.tallyd", "health , 2026-09-30"}, {"ghost.noted", "conversation: boats"},
		{"ghost.noted", "Daily check-in 2026-09-30"}, {"ghost.noted", "a note of my own"}, {"ghost.voiced", "voice note"},
	} {
		ins(`INSERT INTO journal_entries (source, ref, ts, title, body, created_at) VALUES ($1, $2, $3, $2, '', 0)`, j.src, j.title, start+3600)
	}
	sum, err := hw.DayContextFrom(db, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Steps != 8421 {
		t.Fatalf("steps %d: the wrong day's health", sum.Steps)
	}
	if len(sum.Notes) != 2 || sum.Notes[0] != "a note of my own" || sum.Notes[1] != "voice note" {
		t.Fatalf("notes %v", sum.Notes)
	}
}
