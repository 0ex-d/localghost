package main

// THE ARTICLES BEHIND THE HEADLINES, and the day in a few sentences. A feed gives a headline and
// a line or two; a story the box summarises is better told from the article itself. So for each
// story about to be summarised the box reads up to two of its articles' pages, the way a browser
// would, and keeps the paragraphs the page serves anyone. A page that says it is behind a paywall
// (schema.org's isAccessibleForFree: false) keeps only its free part, marked paywalled, and the
// story is told from that and the feeds' words. Then, from the summaries of the day's most-told
// stories, the brief: the day's news in three or four sentences, for the phone's home screen
// (settings news_brief).

import (
	"context"
	"encoding/json"
	"html"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/egress"
	"github.com/LocalGhostDao/localghost/server/internal/feedstat"
	"github.com/LocalGhostDao/localghost/server/internal/oracle"
	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
)

const (
	articlesPerPass  = 12   // pages read per slow pass (every five minutes)
	articlesPerStory = 2    // of each story's reports
	articleKeep      = 8000 // characters of an article read (held until its story is summarised, a day at most)
	articleShort     = 600  // under this, the page gave too little to count as the article
)

var (
	reScript    = regexp.MustCompile(`(?is)<(script|style|noscript|svg|form|nav|header|footer|aside|figure)\b.*?</(script|style|noscript|svg|form|nav|header|footer|aside|figure)>`)
	reParagraph = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)
	reTag       = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpace     = regexp.MustCompile(`\s+`)
	rePaywall   = regexp.MustCompile(`(?i)"isAccessibleForFree"\s*:\s*"?(false|no)"?`)
	boilerplate = []string{"cookie", "subscribe", "sign up", "newsletter", "advertisement", "all rights reserved", "follow us", "share this",
		"read more", "click here", "javascript", "your browser", "log in", "sign in", "terms of use", "privacy policy"}
)

// articleText reads an article's own paragraphs out of its page: within <article> when there is
// one, else <main>, else the whole page; scripts and page furniture out; every <p>, tags
// stripped, entities decoded; boilerplate and lines too short to be prose dropped. paywalled is
// the page's own word that the article is not free.
func articleText(page string) (text string, paywalled bool) {
	paywalled = rePaywall.MatchString(page)
	low := strings.ToLower(page)
	span := page
	for _, tag := range []string{"article", "main"} {
		if i := strings.Index(low, "<"+tag); i >= 0 {
			if j := strings.LastIndex(low, "</"+tag+">"); j > i {
				span = page[i:j]
				break
			}
		}
	}
	span = reScript.ReplaceAllString(span, " ")
	var paras []string
	seen := map[string]bool{}
	size := 0
	for _, m := range reParagraph.FindAllStringSubmatch(span, -1) {
		t := html.UnescapeString(reTag.ReplaceAllString(m[1], " "))
		t = strings.TrimSpace(reSpace.ReplaceAllString(t, " "))
		if len(t) < 40 || seen[t] {
			continue
		}
		lt := strings.ToLower(t)
		skip := false
		for _, b := range boilerplate {
			if strings.Contains(lt, b) && len(t) < 200 {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		seen[t] = true
		paras = append(paras, t)
		size += len(t) + 1
		if size >= articleKeep {
			break
		}
	}
	text = strings.Join(paras, "\n")
	if len(text) > articleKeep {
		text = text[:articleKeep]
	}
	return text, paywalled
}

// articlePass reads the pages of the stories about to be summarised. Returns how many it read.
func articlePass(ctx context.Context, db *poltergres.ReadWrite, client *egress.Client, now time.Time, lg *slog.Logger) (int, error) {
	rows, err := db.Query(`SELECT i.id, i.link, i.story_id, i.feed_id FROM news_items i JOIN news_stories s ON s.id = i.story_id
		WHERE i.body_at = 0 AND i.link LIKE 'http%' AND s.summary = '' AND s.tries < $1 AND (s.sources >= 2 OR s.first_seen < $2)
		ORDER BY s.sources DESC, s.last_seen DESC, i.published DESC LIMIT $3`, newsModelTries, now.Unix()-7200, articlesPerPass*3)
	if err != nil {
		return 0, err
	}
	perStory := map[string]int{}
	read := 0
	var logged []feedstat.Entry
	for _, v := range rows.Vals {
		if len(v) < 4 || v[0] == nil || v[1] == nil || read >= articlesPerPass {
			continue
		}
		story := str(v[2])
		if perStory[story] >= articlesPerStory {
			continue
		}
		perStory[story]++
		id, link := *v[0], *v[1]
		f, ferr := client.GetPage(ctx, "article:"+str(v[3]), link)
		if ferr != nil {
			return read, nil // the context ended
		}
		read++
		status, body := "", ""
		switch {
		case f.Error != "":
			status = "fetch failed: " + clip(f.Error, 100)
		case f.Status < 200 || f.Status > 299:
			status = "HTTP " + strconv.Itoa(f.Status)
		default:
			text, paywalled := articleText(f.Body)
			body = text
			switch {
			case paywalled:
				status = "paywalled"
			case len(text) < articleShort:
				status = "short"
			default:
				status = "ok"
			}
		}
		if err := db.Exec("UPDATE news_items SET body = $2, body_at = $3, body_status = $4 WHERE id = $1", id, body, now.Unix(), status); err != nil {
			return read, err
		}
		logged = append(logged, feedstat.Entry{Source: "article:" + str(v[3]), Kind: feedstat.KindArticle, By: "box", Status: f.Status,
			OK: status == "ok" || status == "paywalled" || status == "short", TookMs: f.TookMs, Bytes: len(f.Body), Items: len(body), Error: errIf(status)})
		time.Sleep(400 * time.Millisecond)
	}
	_ = feedstat.Log(db, now, logged)
	// an article is read for its story's summary and let go once that is written; whatever was not
	// summarised within a day goes too
	_ = db.Exec("UPDATE news_items SET body = '' WHERE body <> '' AND body_at < $1", now.Add(-24*time.Hour).Unix())
	if read > 0 {
		lg.Debug("articles read", "fn", "articlePass", "pages", read)
	}
	return read, nil
}

func errIf(status string) string {
	switch status {
	case "ok", "paywalled", "short":
		return ""
	}
	return status
}

// storyArticle is the best article text a story has: the longest free one, cut to fit a prompt.
func storyArticle(db *poltergres.ReadWrite, storyID int64) string {
	rows, err := db.Query(`SELECT body FROM news_items WHERE story_id = $1 AND body <> '' ORDER BY (body_status = 'ok') DESC, length(body) DESC LIMIT 1`, storyID)
	if err != nil || len(rows.Vals) == 0 || rows.Vals[0][0] == nil {
		return ""
	}
	b := *rows.Vals[0][0]
	if len(b) > 3000 {
		b = b[:3000]
		if i := strings.LastIndexAny(b, ".!?"); i > 2000 {
			b = b[:i+1]
		}
	}
	return b
}

// --- the brief ------------------------------------------------------------------------------

const (
	briefStories = 6
	briefEvery   = 2 * time.Hour // a brief stands this long when its stories have not changed
)

// Brief is the day's news in a few sentences, as the home screen shows it.
type Brief struct {
	At      int64   `json:"at"`
	Text    string  `json:"text"`
	Stories []int64 `json:"stories"`
}

func loadBrief(db *poltergres.ReadWrite) (Brief, bool) {
	var b Brief
	rows, err := db.Query("SELECT value FROM settings WHERE key = 'news_brief'")
	if err != nil || len(rows.Vals) != 1 || rows.Vals[0][0] == nil {
		return b, false
	}
	return b, json.Unmarshal([]byte(*rows.Vals[0][0]), &b) == nil
}

// briefPrompt asks for the day's news from the stories' own summaries and nothing else.
func briefPrompt(summaries []string) string {
	var b strings.Builder
	b.WriteString("Below are the day's main news stories, each already told in a sentence or two, the most widely reported first. Write the day's news as a short brief: three or four plain sentences, the most important first.\n\nSTORIES:\n")
	for _, s := range summaries {
		b.WriteString("- " + s + "\n")
	}
	b.WriteString("\nKeep every number, name and place exactly as the stories give them; add nothing they do not say; no opinion, no headline, no list, no greeting. Reply with the sentences only.")
	return b.String()
}

// groundedBrief holds the brief to the stories, the way a story's summary is held to its reports.
func groundedBrief(out string, summaries []string) (string, bool) {
	s := strings.TrimSpace(strings.Trim(strings.TrimSpace(out), "\"“”"))
	if len(s) < 60 || len(s) > 900 {
		return "", false
	}
	low := strings.ToLower(s)
	for _, b := range badProse {
		if strings.Contains(low, b) {
			return "", false
		}
	}
	if strings.HasPrefix(low, "-") || strings.HasPrefix(low, "•") || strings.Contains(low, "the stories") {
		return "", false
	}
	allowed := map[string]bool{}
	for _, f := range summaries {
		for _, n := range numberRe.FindAllString(f, -1) {
			allowed[n] = true
			allowed[strings.ReplaceAll(n, ",", "")] = true
		}
	}
	for _, n := range numberRe.FindAllString(s, -1) {
		if !allowed[n] && !allowed[strings.ReplaceAll(n, ",", "")] {
			return "", false
		}
	}
	return s, true
}

// briefPass writes the brief when the day's most-told stories changed, or when it is two hours old.
func briefPass(db *poltergres.ReadWrite, oc *oracle.Client, now time.Time, lg *slog.Logger) (bool, error) {
	rows, err := db.Query(`SELECT id, summary FROM news_stories WHERE last_seen >= $1 AND summary <> ''
		ORDER BY sources DESC, last_seen DESC LIMIT $2`, now.Unix()-86400, briefStories)
	if err != nil {
		return false, err
	}
	var ids []int64
	var sums []string
	for _, v := range rows.Vals {
		if len(v) < 2 || v[0] == nil || v[1] == nil {
			continue
		}
		id, _ := strconv.ParseInt(*v[0], 10, 64)
		ids = append(ids, id)
		sums = append(sums, *v[1])
	}
	if len(sums) < 2 {
		return false, nil
	}
	if old, ok := loadBrief(db); ok && sameIDs(old.Stories, ids) && now.Sub(time.Unix(old.At, 0)) < briefEvery {
		return false, nil
	}
	if onGPU, err := oc.OnGPU(); err != nil || !onGPU {
		return false, nil
	}
	resp, err := oc.Infer(oracle.Request{
		Capability: "summarize", Class: oracle.ClassLocalSmall, Priority: oracle.PriorityBackground,
		Input: briefPrompt(sums), MaxTokens: 260, Temperature: 0.2, DeadlineMS: 90000,
	})
	if err != nil || resp.Err != "" {
		return false, nil
	}
	text, ok := groundedBrief(resp.Output, sums)
	if !ok {
		lg.Debug("news brief not grounded, kept the last", "fn", "briefPass")
		return false, nil
	}
	b, _ := json.Marshal(Brief{At: now.Unix(), Text: text, Stories: ids})
	return true, db.Exec("INSERT INTO settings (key, value) VALUES ('news_brief', $1) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value", string(b))
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]int64(nil), a...)
	y := append([]int64(nil), b...)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	sort.Slice(y, func(i, j int) bool { return y[i] < y[j] })
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
