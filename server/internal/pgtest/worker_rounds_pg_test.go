package pgtest

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/framed"
	"github.com/LocalGhostDao/localghost/server/internal/search"
)

// what the model was asked, in order
type calls struct{ log []string }

type fakeCaptioner struct{ c *calls }

func (f fakeCaptioner) Caption(_ context.Context, path string) (string, error) {
	f.c.log = append(f.c.log, "caption "+path[len(path)-9:len(path)-5])
	return "SCENE: a harbour at dusk\nOBJECTS: boats, a lamp", nil
}

type fakeTagger struct{ c *calls }

func (f fakeTagger) Tags(_ context.Context, caption string) ([]search.Tag, error) {
	f.c.log = append(f.c.log, "tags")
	return []search.Tag{{Name: "harbour", Category: "place"}, {Name: "boat", Category: "vehicle"}}, nil
}

func (f fakeTagger) Categorize(_ context.Context, tags []string) ([]search.Tag, error) {
	return nil, nil
}

// A backlog of captions no longer holds every tag pass back: each photo is described, then named
// and tagged in the next round, newest first, before the next caption.
func TestTagPassesFollowTheirCaptions(t *testing.T) {
	db := fresh(t)
	fs := framed.NewStoreDB(db)
	ss := search.NewStore(db)
	lg := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ing := &search.Ingester{Store: ss, Log: lg}
	now := time.Now().UTC().Unix()
	var hashes []string
	for i := 1; i <= 3; i++ {
		h := strings.Repeat(fmt.Sprint(i), 28) + fmt.Sprintf("%04d", i)
		hashes = append(hashes, h)
		if err := fs.InsertFrame(framed.Frame{Hash: h, TakenAt: now - 86400, ArchivePath: "/a/" + h + ".jpg",
			PreviewPath: "/p/" + h + ".webp", ThumbPath: "/t/" + h + ".webp", Kind: "photo", MIME: "image/jpeg",
			TakenSrc: "exif", Source: "test", ReceivedAt: now}); err != nil {
			t.Fatal(err)
		}
		sha := make([]byte, 32)
		sha[0] = byte(i)
		id, _, err := ss.InsertOriginal(search.Original{Source: "image", SHA256: sha, Path: "/a/" + h + ".jpg",
			CapturedAt: time.Unix(now-86400, 0), Daemon: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if err := ss.EnqueueJob("caption", map[string]any{"origId": id, "path": "/p/" + h + ".webp"}); err != nil {
			t.Fatal(err)
		}
	}
	c := &calls{}
	w := &search.Worker{Store: ss, Caption: fakeCaptioner{c}, Tag: fakeTagger{c}, Ingester: ing, Log: lg, Interval: time.Second}
	w.RunOnce(context.Background())
	want := []string{"caption 0003", "tags", "caption 0002", "tags", "caption 0001", "tags"}
	if strings.Join(c.log, ",") != strings.Join(want, ",") {
		t.Fatalf("the model was asked %v; want %v", c.log, want)
	}
	for _, h := range hashes {
		rows, err := db.Query(`SELECT description, display_name, (SELECT count(*) FROM frame_tags WHERE hash = $1) FROM frames WHERE hash = $1`, h)
		if err != nil || len(rows.Vals) != 1 {
			t.Fatalf("%s: %v %v", h, rows, err)
		}
		r := rows.Vals[0]
		if *r[0] != "a harbour at dusk" || !strings.HasSuffix(*r[1], "harbour boat") || *r[2] != "2" {
			t.Fatalf("%s: description %q, name %q, tags %s", h, *r[0], *r[1], *r[2])
		}
	}
	if rows, _ := db.Query(`SELECT count(*) FROM search.jobs WHERE kind IN ('caption','tag')`); *rows.Vals[0][0] != "0" {
		t.Fatalf("%s caption or tag jobs left", *rows.Vals[0][0])
	}
	// what `ghost.searchd queue` reports
	pp, err := ss.PhotoProgress()
	if err != nil || pp != (search.PhotoProgress{Photos: 3, Described: 3, Named: 3, Tagged: 3, DescribedHour: 3, TaggedHour: 3}) {
		t.Fatalf("PhotoProgress %+v %v", pp, err)
	}
	if w.Lanes().Resting {
		t.Fatal("lanes resting after a clean run")
	}

	// the model not there: the job keeps its lives, the lanes rest, and `queue` says why
	if err := ss.EnqueueJob("caption", map[string]any{"origId": 1, "path": "/p/x.webp"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO search.jobs (kind, payload, attempts, last_error) VALUES ('tag', '{}'::jsonb, 5, 'caption implausibly short')`); err != nil {
		t.Fatal(err)
	}
	down := &search.Worker{Store: ss, Caption: downCaptioner{}, Tag: fakeTagger{c}, Ingester: ing, Log: lg, Interval: time.Second}
	down.RunOnce(context.Background())
	if l := down.Lanes(); !l.Resting || !strings.Contains(l.Why, "no backend") || l.RestingSince == 0 {
		t.Fatalf("lanes %+v", l)
	}
	kinds, err := ss.JobKinds()
	if err != nil {
		t.Fatal(err)
	}
	if k := kinds["caption"]; k.Waiting != 1 || k.Runnable != 0 || k.Parked != 0 {
		t.Fatalf("caption %+v (the unclaimed job waits out the rest)", k)
	}
	if k := kinds["tag"]; k.Parked != 1 || k.LastError != "caption implausibly short" {
		t.Fatalf("tag %+v", k)
	}
}

// An engine that sees no images holds only the caption lane: the tag pass and the category
// backfill are text and go on, and the caption keeps its lives.
func TestNoVisionHoldsOnlyCaptions(t *testing.T) {
	db := fresh(t)
	ss := search.NewStore(db)
	lg := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ing := &search.Ingester{Store: ss, Log: lg}
	if err := ss.EnqueueJob("caption", map[string]any{"origId": 1, "path": "/p/x.webp"}); err != nil {
		t.Fatal(err)
	}
	sha := make([]byte, 32)
	sha[0] = 9
	id, _, err := ss.InsertOriginal(search.Original{Source: "image", SHA256: sha, Path: "/a/x.jpg", CapturedAt: time.Unix(1, 0), Daemon: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.EnqueueJob("tag", map[string]any{"origId": id, "path": "/p/" + strings.Repeat("a", 32) + ".webp", "caption": "SCENE: a boat", "captured": 1}); err != nil {
		t.Fatal(err)
	}
	c := &calls{}
	w := &search.Worker{Store: ss, Caption: blindCaptioner{}, Tag: fakeTagger{c}, Ingester: ing, Log: lg, Interval: time.Second}
	w.RunOnce(context.Background())
	if strings.Join(c.log, ",") != "tags" {
		t.Fatalf("the model was asked %v; the tag pass should run", c.log)
	}
	if l := w.CaptionLane(); !l.Resting || !strings.Contains(l.Why, "no vision") {
		t.Fatalf("caption lane %+v", l)
	}
	if l := w.Lanes(); l.Resting {
		t.Fatalf("every model lane resting for want of vision: %+v", l)
	}
	kinds, _ := ss.JobKinds()
	if k := kinds["caption"]; k.Waiting != 1 || k.Parked != 0 {
		t.Fatalf("caption %+v", k)
	}
	if _, ok := kinds["tag"]; ok {
		t.Fatalf("the tag job is still there: %+v", kinds["tag"])
	}
}

type blindCaptioner struct{}

func (blindCaptioner) Caption(context.Context, string) (string, error) {
	return "", fmt.Errorf("no vision: llama-server takes no images (started without the mmproj projector?): image input is not supported")
}

type downCaptioner struct{}

func (downCaptioner) Caption(context.Context, string) (string, error) {
	return "", fmt.Errorf("no backend for class local-small")
}
