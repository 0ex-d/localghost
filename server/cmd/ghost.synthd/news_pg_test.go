package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/egress"
	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// Two feeds tell the same story, a third answers with an HTML page, a fourth is not fetched: the
// entries land once, the story counts two outlets, the feeds' health says what happened, the
// digest goes out once an hour-day, and the chat finds the story by its words.
func TestNewsIngestStoriesAndDigest(t *testing.T) {
	db := pgFresh(t, "lgtest_synthd_news")
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := seedFeeds(db); err != nil {
		t.Fatal(err)
	}
	if err := seedFeeds(db); err != nil { // twice is fine
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)
	rss := func(title, link, desc string) string {
		return `<rss version="2.0"><channel><title>x</title><item><title>` + title + `</title><link>` + link + `</link><description>` + desc + `</description><pubDate>` + now.Add(-time.Hour).Format(time.RFC1123Z) + `</pubDate></item></channel></rss>`
	}
	batch := map[string]any{"fetchedAt": now.Unix(), "feeds": []map[string]any{
		{"id": "bbc", "status": 200, "body": rss("Minister resigns over leaked memo", "https://bbc/a", "The minister resigned on Tuesday after a memo leaked.")},
		{"id": "guardian", "status": 200, "body": rss("Leaked memo: the minister's resignation", "https://guardian/b", "A leaked memo ended the minister's week.")},
		{"id": "ft", "status": 200, "body": "<html><body>please accept cookies</body></html>"},
		{"id": "npr", "status": 0, "error": "timeout"},
	}}
	raw, _ := json.Marshal(batch)
	res, err := ingestFetched(db, raw, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Feeds != 4 || res.OK != 2 || res.NewItems != 2 || res.NewStories != 1 || len(res.Failed) != 2 {
		t.Fatalf("%+v", res)
	}
	// the same batch again: nothing new
	res, err = ingestFetched(db, raw, now.Add(time.Minute))
	if err != nil || res.NewItems != 0 || res.NewStories != 0 {
		t.Fatalf("again: %+v %v", res, err)
	}
	st, err := newsStatus(db, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if st.FeedsOKLast3h != 2 || st.Items7d != 2 || st.Stories24h != 1 {
		t.Fatalf("status: %+v", st)
	}
	byID := map[string]newsFeed{}
	for _, f := range st.Feeds {
		byID[f.ID] = f
	}
	if byID["bbc"].Status != "ok" || byID["bbc"].Items != 1 || !strings.HasPrefix(byID["ft"].Status, "not a feed") || byID["ft"].Failures != 2 || byID["npr"].Status != "fetch failed: timeout" {
		t.Fatalf("feeds: %+v", byID)
	}
	rows, _ := db.Query("SELECT sources, title FROM news_stories")
	if len(rows.Vals) != 1 || *rows.Vals[0][0] != "2" {
		t.Fatalf("story: %v", rows.Vals)
	}
	// the chat finds it by its words
	items := newsItemsFor(db, []string{"minister", "memo"}, now)
	if len(items) != 2 || items[0].Source != "news" || !strings.Contains(items[0].Snippet+items[1].Snippet, "Minister resigns") {
		t.Fatalf("news source: %+v", items)
	}
	if items := newsItemsFor(db, []string{"ferries", "corfu"}, now); len(items) != 0 {
		t.Fatalf("an unrelated question matched: %+v", items)
	}
	// the digest, in the person's zone: 07:00 in Athens is 04:00 UTC
	_ = db.Exec("INSERT INTO settings (key, value) VALUES ('local_tz','Europe/Athens')")
	if _, _, due := digestDue(db, time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC)); due {
		t.Fatal("07:00 UTC is 10:00 in Athens: no digest")
	}
	kind, day, due := digestDue(db, time.Date(2026, 10, 1, 4, 10, 0, 0, time.UTC))
	if !due || kind != "07:00" || day != "2026-10-01" {
		t.Fatalf("due: %v %s %s", due, kind, day)
	}
	var posted []hw.Notification
	produce := func(n hw.Notification) error { posted = append(posted, n); return nil }
	n, err := postDigest(db, now, kind, day, produce, lg)
	if err != nil || n != 1 || len(posted) != 1 || posted[0].Kind != "news" || !strings.Contains(posted[0].Body, "Minister resigns") || !strings.Contains(posted[0].Body, "(2 outlets)") {
		t.Fatalf("digest: %d %v %+v", n, err, posted)
	}
	if _, _, due := digestDue(db, time.Date(2026, 10, 1, 4, 20, 0, 0, time.UTC)); due {
		t.Fatal("the same digest offered twice in one day")
	}
	// nothing new since: the next digest sends nothing and still marks the hour
	n, err = postDigest(db, now.Add(12*time.Hour), "19:00", day, produce, lg)
	if err != nil || n != 0 || len(posted) != 1 {
		t.Fatalf("empty digest: %d %v %d", n, err, len(posted))
	}
	// the feed commands' SQL
	if err := db.Exec("INSERT INTO news_feeds (id, name, url, added_at) VALUES ($1,$2,$3,$4) ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, url = EXCLUDED.url, enabled = true", "x", "X", "https://x/feed", now.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE news_feeds SET enabled = $2 WHERE id = $1", "x", false); err != nil {
		t.Fatal(err)
	}
}

// The box fetches the feeds itself only when the phone is not on Wi-Fi: with the phone's word
// "wifi" five minutes old it leaves them; on "mobile" it fetches the due feeds from the box and
// the batch says so.
func TestNewsFetchedByTheBoxWhenThePhoneIsAway(t *testing.T) {
	db := pgFresh(t, "lgtest_synthd_newsbox")
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<rss version="2.0"><channel><title>x</title><item><title>Box fetched this ` + r.URL.Path + `</title><link>https://x` + r.URL.Path + `</link><pubDate>` + now.Add(-time.Hour).Format(time.RFC1123Z) + `</pubDate></item></channel></rss>`))
	}))
	defer srv.Close()
	// two feeds of our own, the defaults off
	_ = db.Exec("INSERT INTO settings (key, value) VALUES ('news_seeded','1')")
	_ = db.Exec("INSERT INTO news_feeds (id, name, url, added_at) VALUES ('a','A',$1,1),('b','B',$2,1)", srv.URL+"/a", srv.URL+"/b")
	client := egress.New()
	// the phone on Wi-Fi five minutes ago: left to the phone
	_ = db.Exec("INSERT INTO settings (key, value) VALUES ('phone_net','wifi'),('phone_seen',$1)", strconv.FormatInt(now.Add(-5*time.Minute).Unix(), 10))
	what, err := newsFetchByBox(context.Background(), db, client, now, false, lg)
	if err != nil || !strings.HasPrefix(what, "left to the phone") {
		t.Fatalf("%q %v", what, err)
	}
	// on mobile: the box fetches both
	_ = db.Exec("UPDATE settings SET value = 'mobile' WHERE key = 'phone_net'")
	what, err = newsFetchByBox(context.Background(), db, client, now, false, lg)
	if err != nil || !strings.HasPrefix(what, "fetched 2 feeds (2 answered, 2 new entries)") {
		t.Fatalf("%q %v", what, err)
	}
	st, _ := newsStatus(db, now)
	if st.LastBy != "box" || st.LastByAt != now.Unix() || st.FeedsOKLast3h != 2 {
		t.Fatalf("%+v", st)
	}
	// fetched just now: nothing due, even on mobile
	if what, _ := newsFetchByBox(context.Background(), db, client, now.Add(time.Minute), false, lg); !strings.HasPrefix(what, "nothing due") {
		t.Fatalf("%q", what)
	}
	// forced: fetched again whatever the marks say
	if what, _ := newsFetchByBox(context.Background(), db, client, now.Add(time.Minute), true, lg); !strings.HasPrefix(what, "fetched 2 feeds") {
		t.Fatalf("%q", what)
	}
}
