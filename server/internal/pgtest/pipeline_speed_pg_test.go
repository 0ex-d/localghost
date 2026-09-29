package pgtest

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// oldStageSQL is the stage count as it was before 29 Sep 2026 (two EXISTS per frame, inlined into
// five FILTERs). Kept here only to prove the grouped join counts exactly the same.
const oldStageSQL = `
		WITH ver AS (SELECT coalesce(max(pipe_ver), 0) AS v FROM frames)
		SELECT (SELECT v FROM ver),
		       count(*) FILTER (WHERE kind = 'photo'),
		       count(*) FILTER (WHERE kind = 'video'),
		       count(*) FILTER (WHERE kind NOT IN ('photo','video')),
		       count(*) FILTER (WHERE staged AND pipe_ver >= (SELECT v FROM ver)),
		       count(*) FILTER (WHERE staged AND preview_path <> '' AND thumb_path <> ''),
		       count(*) FILTER (WHERE staged AND description <> ''),
		       count(*) FILTER (WHERE staged AND display_name <> ''),
		       count(*) FILTER (WHERE staged AND tagged),
		       count(*) FILTER (WHERE staged AND tagged AND categorised),
		       count(*) FILTER (WHERE staged AND pipe_ver >= (SELECT v FROM ver) AND preview_path <> '' AND thumb_path <> ''
		                          AND description <> '' AND display_name <> '' AND tagged AND categorised)
		FROM (SELECT f.*, f.kind IN ('photo','video') AS staged,
		             EXISTS (SELECT 1 FROM frame_tags t WHERE t.hash = f.hash) AS tagged,
		             NOT EXISTS (SELECT 1 FROM frame_tags t WHERE t.hash = f.hash AND t.category = '' AND t.source <> 'user_removed') AS categorised
		      FROM frames f) x`

// The Box Status counts from one grouped read of frame_tags equal the old per-frame EXISTS counts
// on a library with every case in it (untagged, fully categorised, one tag waiting, a waiting tag
// the person removed, other kinds, older pipeline versions), and the new query is faster.
func TestPipelineStagesGroupedJoinCountsTheSame(t *testing.T) {
	db := fresh(t)
	const n = 6000
	err := db.ExecSimple(fmt.Sprintf(`
		INSERT INTO frames (hash, archive_path, kind, pipe_ver, preview_path, thumb_path, description, display_name, described_at)
		SELECT 'h'||g, '/a/'||g,
		       CASE WHEN g %% 11 = 0 THEN 'video' WHEN g %% 37 = 0 THEN 'doc' ELSE 'photo' END,
		       CASE WHEN g %% 13 = 0 THEN 1 ELSE 2 END,
		       CASE WHEN g %% 17 = 0 THEN '' ELSE 'p' END, 't',
		       CASE WHEN g %% 5 = 0 THEN '' ELSE 'described' END,
		       CASE WHEN g %% 19 = 0 THEN '' ELSE 'name' END,
		       %d - g
		FROM generate_series(1, %d) g;
		-- tags: none for g %% 7 = 0; four each otherwise; one waiting for a category on g %% 3 = 0,
		-- and on g %% 9 = 0 that waiting one is a tombstone the person removed
		INSERT INTO frame_tags (hash, tag, source, created_at, category)
		SELECT 'h'||g, 'tag'||k,
		       CASE WHEN k = 1 AND g %% 9 = 0 THEN 'user_removed' ELSE 'model' END, 0,
		       CASE WHEN k = 1 AND g %% 3 = 0 THEN '' ELSE 'things' END
		FROM generate_series(1, %d) g, generate_series(1, 4) k
		WHERE g %% 7 <> 0;
		ANALYZE frames; ANALYZE frame_tags;`, time.Now().Unix(), n, n))
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	rows, err := db.Query(oldStageSQL)
	oldTook := time.Since(t0)
	if err != nil || len(rows.Vals) != 1 {
		t.Fatalf("old query: %v", err)
	}
	old := make([]int, len(rows.Vals[0]))
	for i, v := range rows.Vals[0] {
		old[i], _ = strconv.Atoi(*v)
	}
	t0 = time.Now()
	p, err := hw.PipelineProgressFrom(db)
	newTook := time.Since(t0)
	if err != nil {
		t.Fatal(err)
	}
	got := []int{p.PipelineVersion, p.Photos, p.Videos, p.Other, p.Derived.Done, p.Previewed.Done, p.Described.Done,
		p.Titled.Done, p.Tagged.Done, p.Categorised.Done, p.AtLatest.Done}
	for i := range got {
		if got[i] != old[i] {
			t.Fatalf("column %d: grouped join %d, per-frame EXISTS %d (all: new %v old %v)", i, got[i], old[i], got, old)
		}
	}
	if p.Tagged.Done == 0 || p.Categorised.Done == p.Tagged.Done || p.Other == 0 || p.Derived.Done == p.Total {
		t.Fatalf("the data did not exercise every case: %+v", got)
	}
	t.Logf("old %s, new %s (the whole feed, jobs and converge included), counts %v", oldTook, newTook, got)
}
