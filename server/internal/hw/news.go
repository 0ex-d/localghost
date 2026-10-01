package hw

import (
	"strconv"

	"github.com/LocalGhostDao/localghost/server/internal/feeds"
)

// NewsItem is one outlet's telling of a story, as the phone lists it (the link is the phone's to
// open; the box never fetches it).
type NewsItem struct {
	Feed      string `json:"feed"`
	Outlet    string `json:"outlet"`
	Title     string `json:"title"`
	Link      string `json:"link"`
	Summary   string `json:"summary"`
	Published int64  `json:"published"`
}

// NewsStory is one story and the outlets telling it.
type NewsStory struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Summary   string     `json:"summary"` // the model's, "" until written
	Sources   int        `json:"sources"`
	FirstSeen int64      `json:"firstSeen"`
	LastSeen  int64      `json:"lastSeen"`
	Items     []NewsItem `json:"items"`
}

// NewsStories is the stories seen since a time, the most-told and newest first, each with its
// outlets' entries (eight at most).
func NewsStories(c Querier, since int64, limit int) ([]NewsStory, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := c.Query(`SELECT id, title, summary, sources, first_seen, last_seen FROM news_stories
		WHERE last_seen >= $1 ORDER BY last_seen DESC, sources DESC LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	out := make([]NewsStory, 0, len(rows.Vals))
	for _, v := range rows.Vals {
		if len(v) < 6 || v[0] == nil {
			continue
		}
		var s NewsStory
		s.ID, _ = strconv.ParseInt(*v[0], 10, 64)
		s.Title, s.Summary = deref(v[1]), deref(v[2])
		s.Sources, _ = strconv.Atoi(deref(v[3]))
		s.FirstSeen, _ = strconv.ParseInt(deref(v[4]), 10, 64)
		s.LastSeen, _ = strconv.ParseInt(deref(v[5]), 10, 64)
		items, err := c.Query(`SELECT i.feed_id, f.name, i.title, i.link, i.summary, i.published FROM news_items i
			JOIN news_feeds f ON f.id = i.feed_id WHERE i.story_id = $1 ORDER BY i.published DESC LIMIT 8`, s.ID)
		if err != nil {
			return nil, err
		}
		for _, iv := range items.Vals {
			if len(iv) < 6 {
				continue
			}
			var it NewsItem
			it.Feed, it.Outlet, it.Title, it.Link, it.Summary = deref(iv[0]), deref(iv[1]), deref(iv[2]), deref(iv[3]), deref(iv[4])
			it.Published, _ = strconv.ParseInt(deref(iv[5]), 10, 64)
			s.Items = append(s.Items, it)
		}
		out = append(out, s)
	}
	return out, nil
}

// FeedList is the enabled feeds for the phone to fetch; a box synthd has not seeded yet gets the
// default list, so the first fetch happens before the first drain.
func FeedList(c Querier) ([]feeds.Source, error) {
	rows, err := c.Query("SELECT id, name, url FROM news_feeds WHERE enabled ORDER BY id")
	if err != nil {
		return nil, err
	}
	var out []feeds.Source
	for _, v := range rows.Vals {
		if len(v) < 3 || v[0] == nil || v[2] == nil {
			continue
		}
		out = append(out, feeds.Source{ID: *v[0], Name: deref(v[1]), URL: *v[2]})
	}
	seeded, serr := c.Query("SELECT 1 FROM settings WHERE key = 'news_seeded'")
	if len(out) == 0 && (serr != nil || len(seeded.Vals) == 0) {
		return feeds.DefaultSources(), nil
	}
	return out, nil
}

// NewsMarks is when the feeds were last fetched and the last digest went, for the screen's header.
func NewsMarks(c Querier) (lastFetch, lastDigest int64) {
	if rows, err := c.Query("SELECT coalesce(max(last_fetch),0) FROM news_feeds"); err == nil && len(rows.Vals) == 1 && rows.Vals[0][0] != nil {
		lastFetch, _ = strconv.ParseInt(*rows.Vals[0][0], 10, 64)
	}
	if rows, err := c.Query("SELECT coalesce(max(at),0) FROM news_digests"); err == nil && len(rows.Vals) == 1 && rows.Vals[0][0] != nil {
		lastDigest, _ = strconv.ParseInt(*rows.Vals[0][0], 10, 64)
	}
	return
}
