package hw

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestInterestTerms(t *testing.T) {
	terms := InterestTerms([]WeightedText{
		{Text: "Vlad's work. Vlad is the CTO of CryptoCompare and builds blockchain data products.", Weight: 2},
		{Text: "Vlad plays strategy games and Dungeons & Dragons.", Weight: 2},
		{Text: "Sailing plans", Weight: 1},
		{Text: "Sailing in Corfu", Weight: 1},
		{Text: "Coffee", Weight: 1},
		{Text: "Vlad", Weight: -1},
	})
	for _, want := range []string{"cryptocompare", "blockchain", "strategy", "dragon", "sailing"} {
		if _, ok := terms[want]; !ok {
			t.Errorf("%q missing: %v", want, terms)
		}
	}
	for _, not := range []string{"vlad", "coffee", "the", "work", "corfu"} {
		if _, ok := terms[not]; ok {
			t.Errorf("%q kept: %v", not, terms)
		}
	}
	if terms["dragon"] != "Dragons" || terms["sailing"] != "Sailing" {
		t.Fatalf("display forms %v", terms)
	}
}

func TestPickStories(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	terms := map[string]string{"blockchain": "blockchain", "sailing": "sailing", "romania": "Romania"}
	stories := []NewsStory{
		{ID: 1, Title: "Romania's sailing team wins", Sources: 2, LastSeen: now.Unix() - 100},
		{ID: 2, Title: "Blockchain bill passes", Sources: 9, LastSeen: now.Unix() - 100},
		{ID: 3, Title: "Blockchain firm hacked", Summary: "A blockchain firm lost funds.\n- point", Sources: 3, LastSeen: now.Unix() - 50},
		{ID: 4, Title: "Weather turns", Sources: 12, LastSeen: now.Unix()},
		{ID: 5, Title: "Sailing in 2019", Sources: 1, LastSeen: now.Unix() - 3*86400},
	}
	got := PickStories(stories, terms, []int64{2}, now)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("%+v", got)
	}
	if got[0].Why != "you mention Romania, sailing" || got[1].Lead != "A blockchain firm lost funds." {
		t.Fatalf("%+v", got)
	}
	if len(PickStories(stories, nil, nil, now)) != 0 {
		t.Fatal("no terms, no picks")
	}
}

func TestForYouStillFresh(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	f := ForYou{At: now.Unix() - 60, From: now.Unix() - 600, lat: 51.5, lon: -0.12}
	b, _ := f.MarshalCopy()
	if strings.Contains(string(mustJSON(f)), "51.5") || !strings.Contains(string(b), "51.5") {
		t.Fatal("the position goes to Redis, never to the phone")
	}
	g, ok := UnmarshalForYouCopy(b)
	if !ok || !g.StillFresh(now, f.From, 51.5, -0.12) {
		t.Fatal("same point")
	}
	if !g.StillFresh(now, now.Unix(), 51.503, -0.12) {
		t.Fatal("moved 300 m")
	}
	if g.StillFresh(now, now.Unix(), 51.52, -0.12) {
		t.Fatal("moved 2 km")
	}
	if g.StillFresh(now.Add(ForYouFresh), f.From, 51.5, -0.12) {
		t.Fatal("too old")
	}
	none := ForYou{At: now.Unix() - 60}
	if none.StillFresh(now, now.Unix()-30, 51.5, -0.12) {
		t.Fatal("a fresh point where there was none")
	}
	if !none.StillFresh(now, now.Unix()-8*3600, 51.5, -0.12) {
		t.Fatal("an old point stays old")
	}
	if lastPart("Europe / Greece / Corfu / Kassiopi") != "Kassiopi" || lastPart("") != "" {
		t.Fatal("place")
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
