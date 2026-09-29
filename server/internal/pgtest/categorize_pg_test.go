package pgtest

import (
	"encoding/json"
	"testing"

	"github.com/LocalGhostDao/localghost/server/internal/search"
)

// The category backfill queue against a real Postgres: one statement queues a job per frame with
// uncategorised tags (the tags as a JSON list, the payload doCategorize reads), respects the
// limit, never queues a frame twice, and a frame whose tags all have a category , "other"
// included, the verdict for a tag the model placed nowhere , is not queued at all.
func TestCategorizeQueueAgainstPostgres(t *testing.T) {
	db := fresh(t)
	ss := search.NewStore(db)
	ins := func(hash, tag, cat string) {
		t.Helper()
		if err := db.Exec(`INSERT INTO frame_tags (hash, tag, source, created_at, category) VALUES ($1, $2, 'model', 1, $3)`, hash, tag, cat); err != nil {
			t.Fatal(err)
		}
	}
	ins("aaaa", "beach", "")
	ins("aaaa", "zorblax", "")
	ins("aaaa", "sea", "nature")
	ins("bbbb", "boat", "")
	ins("cccc", "dog", "")
	ins("dddd", "blob", search.OtherCategory) // asked before, placed nowhere: done
	ins("dddd", "cat", "animal")

	n, err := ss.EnqueueCategorize(2)
	if err != nil || n != 2 {
		t.Fatalf("first batch: %d %v", n, err)
	}
	if n, err = ss.EnqueueCategorize(10); err != nil || n != 1 {
		t.Fatalf("second batch (the rest, none twice): %d %v", n, err)
	}
	if n, err = ss.EnqueueCategorize(10); err != nil || n != 0 {
		t.Fatalf("third batch: %d %v", n, err)
	}
	rows, err := db.Query(`SELECT payload::text FROM search.jobs WHERE kind = 'categorize' AND payload->>'hash' = 'aaaa'`)
	if err != nil || len(rows.Vals) != 1 {
		t.Fatalf("aaaa's job: %v %v", rows, err)
	}
	var p struct {
		Hash string   `json:"hash"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(*rows.Vals[0][0]), &p); err != nil || p.Hash != "aaaa" || len(p.Tags) != 2 || p.Tags[0] != "beach" || p.Tags[1] != "zorblax" {
		t.Fatalf("payload %s: %+v %v", *rows.Vals[0][0], p, err)
	}
	if rows, _ := db.Query(`SELECT count(*) FROM search.jobs WHERE payload->>'hash' = 'dddd'`); *rows.Vals[0][0] != "0" {
		t.Fatal("a frame whose tags all have a category was queued")
	}
	// a job parked by five failures (the old parser) is given back and its frame queued again
	if err := db.Exec(`UPDATE search.jobs SET attempts = 5 WHERE payload->>'hash' = 'bbbb'`); err != nil {
		t.Fatal(err)
	}
	if n, err = ss.EnqueueCategorize(10); err != nil || n != 1 {
		t.Fatalf("after a parked job: %d %v", n, err)
	}
	if rows, _ := db.Query(`SELECT attempts FROM search.jobs WHERE payload->>'hash' = 'bbbb'`); len(rows.Vals) != 1 || *rows.Vals[0][0] != "0" {
		t.Fatalf("bbbb not queued afresh: %v", rows)
	}
}
