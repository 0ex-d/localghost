package pgtest

import (
	"strings"
	"testing"

	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

// A health batch with a heart-rate reading twice at the same second (a watch and a phone both
// writing) used to make Postgres refuse the whole statement and the day never landed; the day's
// journal line is replaced when a later upload refines the day.
func TestHealthIngestAgainstPostgres(t *testing.T) {
	db := fresh(t)
	raw := []byte(`{"days":[{"day":"2026-09-30","metrics":{"steps":8421,"sleep_minutes":402,"hr_avg":61,"hr_max":140}},
		{"day":"bad-day","metrics":{"steps":1}},{"day":"2026-09-29","metrics":{"steps":1200,"weird name":3}}],
		"samples":[{"metric":"heart_rate","ts":1790800000,"value":60},{"metric":"heart_rate","ts":1790800000,"value":62},
		{"metric":"heart_rate","ts":1790800300,"value":70},{"metric":"x;drop","ts":1,"value":1},{"metric":"heart_rate","ts":0,"value":1}]}`)
	res, err := tally.Ingest(db, raw)
	if err != nil {
		t.Fatal(err)
	}
	if res.Days != 2 || res.Metrics != 5 || res.Samples != 2 || res.Dropped != 4 || res.NewestDay != "2026-09-30" || res.OldestDay != "2026-09-29" {
		t.Fatalf("result %+v", res)
	}
	rows, _ := db.Query("SELECT value FROM health_samples WHERE metric = 'heart_rate' AND ts = 1790800000")
	if len(rows.Vals) != 1 || *rows.Vals[0][0] != "62" {
		t.Fatalf("the later reading at the same second should win: %v", rows)
	}
	rows, _ = db.Query("SELECT body FROM journal_entries WHERE source = 'ghost.tallyd' AND ref = 'health:2026-09-30'")
	if len(rows.Vals) != 1 || !strings.Contains(*rows.Vals[0][0], "Slept 6h42m. 8421 steps.") || !strings.Contains(*rows.Vals[0][0], "peak 140") {
		t.Fatalf("journal %v", rows)
	}
	// a refinement of the same day replaces the line
	if _, err := tally.Ingest(db, []byte(`{"days":[{"day":"2026-09-30","metrics":{"steps":9000,"sleep_minutes":402}}]}`)); err != nil {
		t.Fatal(err)
	}
	rows, _ = db.Query("SELECT body FROM journal_entries WHERE source = 'ghost.tallyd' AND ref = 'health:2026-09-30'")
	if len(rows.Vals) != 1 || !strings.Contains(*rows.Vals[0][0], "9000 steps") {
		t.Fatalf("journal not refined: %v", rows)
	}
	st, err := tally.Query(db)
	if err != nil || st.Days != 2 || st.Samples != 2 || st.NewestDay != "2026-09-30" || st.Metrics["steps"] != "2026-09-30" || st.LastValues["steps"] != 9000 || st.Metrics["hr_avg"] != "2026-09-30" {
		t.Fatalf("status %+v %v", st, err)
	}
	// an older phone's "calories" (Health Connect's resting estimate) is not kept; active kcal is,
	// and the day's line says so
	if _, err := tally.Ingest(db, []byte(`{"days":[{"day":"2026-09-28","metrics":{"steps":7000,"calories":1564.5,"active_calories":456}}]}`)); err != nil {
		t.Fatal(err)
	}
	rows, _ = db.Query("SELECT string_agg(metric, ',' ORDER BY metric) FROM health_metrics WHERE day = '2026-09-28'")
	if len(rows.Vals) != 1 || *rows.Vals[0][0] != "active_calories,steps" {
		t.Fatalf("metrics %v", rows.Vals)
	}
	rows, _ = db.Query("SELECT body FROM journal_entries WHERE ref = 'health:2026-09-28'")
	if len(rows.Vals) != 1 || *rows.Vals[0][0] != "7000 steps. 456 active kcal." {
		t.Fatalf("journal %q", *rows.Vals[0][0])
	}
	// not a batch: reported, no error (moved aside, never retried)
	if res, err := tally.Ingest(db, []byte(`[1,2]`)); err != nil || !res.Unparsable {
		t.Fatalf("unparsable %+v %v", res, err)
	}
}
