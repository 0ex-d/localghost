package hw

import (
	"encoding/json"
	"strconv"

	"github.com/LocalGhostDao/localghost/server/internal/feeds"
)

// NewsItem is one outlet's telling of a story, as the phone lists it (the link is the phone's to
// open; the box reads the article behind it only for a story it summarises).
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

// NewsBrief is the day's news as synthd last wrote it from the summaries of the most-told
// stories (settings news_brief): "- point" lines, the nth telling stories[n] when the counts
// agree (a brief from before the points is a paragraph); "" before the first.
func NewsBrief(c Querier) (text string, at int64, stories []int64) {
	rows, err := c.Query("SELECT value FROM settings WHERE key = 'news_brief'")
	if err != nil || len(rows.Vals) != 1 || rows.Vals[0][0] == nil {
		return "", 0, nil
	}
	var b struct {
		At      int64   `json:"at"`
		Text    string  `json:"text"`
		Stories []int64 `json:"stories"`
	}
	if json.Unmarshal([]byte(*rows.Vals[0][0]), &b) != nil {
		return "", 0, nil
	}
	return b.Text, b.At, b.Stories
}

// NewsDoc is /v1/news: the stories, when the feeds were fetched and the digest went, and the
// brief. The box keeps the last two days' in Redis (hw/hot.go), so the phone has it at once.
type NewsDoc struct {
	Stories    []NewsStory `json:"stories"`
	LastFetch  int64       `json:"lastFetch"`
	LastDigest int64       `json:"lastDigest"`
	// Brief is the day's news as one point per story ("- " lines), written from the most-told
	// stories' summaries (the home screen's); "" before the first. BriefStories are the stories
	// the points tell, in order.
	Brief        string  `json:"brief"`
	BriefAt      int64   `json:"briefAt"`
	BriefStories []int64 `json:"briefStories"`
	Since        int64   `json:"since"` // how far back the stories go
	BuiltAt      int64   `json:"builtAt"`
}

// NewsDocNow reads /v1/news from Postgres.
func NewsDocNow(c Querier, since int64, limit int, now int64) (NewsDoc, error) {
	stories, err := NewsStories(c, since, limit)
	if err != nil {
		return NewsDoc{}, err
	}
	if stories == nil {
		stories = []NewsStory{}
	}
	d := NewsDoc{Stories: stories, Since: since, BuiltAt: now}
	d.LastFetch, d.LastDigest = NewsMarks(c)
	d.Brief, d.BriefAt, d.BriefStories = NewsBrief(c)
	if d.BriefStories == nil {
		d.BriefStories = []int64{}
	}
	return d, nil
}

// Within is the doc cut to the stories seen since a time (the cached two days serve any later
// since).
func (d NewsDoc) Within(since int64) NewsDoc {
	if since <= d.Since {
		return d
	}
	out := d
	out.Stories = make([]NewsStory, 0, len(d.Stories))
	for _, s := range d.Stories {
		if s.LastSeen >= since {
			out.Stories = append(out.Stories, s)
		}
	}
	out.Since = since
	return out
}
