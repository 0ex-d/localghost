package main

import (
	"strings"
	"testing"

	"github.com/LocalGhostDao/localghost/server/internal/dayroute"
)

func TestPlacesCounted(t *testing.T) {
	stay := func(name string, from, to int64) dayroute.Stay { return dayroute.Stay{Name: name, From: from, To: to} }
	days := []dayroute.Day{
		{Day: "2026-09-05", Stays: []dayroute.Stay{stay("Regent's Park", 0, 7200), stay("", 0, 100)}}, // a Saturday
		{Day: "2026-09-12", Stays: []dayroute.Stay{stay("near Regent's Park", 0, 3600)}},
		{Day: "2026-09-19", Stays: []dayroute.Stay{stay("Regent's Park", 0, 3600)}},
		{Day: "2026-09-24", Stays: []dayroute.Stay{stay("Regent's Park", 0, 1800), stay("Kassiopi", 0, 600)}},
	}
	aggs := aggregatePlaces(days)
	rp := aggs["regent's park"]
	if rp == nil || len(rp.Days) != 4 || rp.Seconds != 16200 || aggs["kassiopi"] == nil || len(aggs) != 2 {
		t.Fatalf("%+v", aggs)
	}
	rp.Photos = 12
	got := placeBody("Vlad", rp)
	want := "Vlad has been at Regent's Park on 4 days, from 5 September 2026 to 24 September 2026, about 4 hours in all, most often on a Saturday. 12 photos taken there."
	if got != want {
		t.Fatalf("%q", got)
	}
	if b := placeBody("", aggs["kassiopi"]); !strings.HasPrefix(b, "I have been at Kassiopi on 1 day, on 24 September 2026.") {
		t.Fatalf("%q", b)
	}
	if lastPlacePart("Europe / Greece / Corfu / Kassiopi") != "Kassiopi" {
		t.Fatal("place")
	}
}

func TestInsightPromptAndParse(t *testing.T) {
	facts := []string{
		"In the last 30 days Vlad was at Regent's Park on 9 days.",
		"Vlad walked about 84 km in the last 30 days.",
		"Vlad walked about 51 km in the 30 days before that.",
	}
	p := insightPrompt("Vlad", facts)
	if !strings.Contains(p, "about Vlad by name") || !strings.Contains(p, "- Vlad walked about 84 km") || !strings.Contains(p, "no guessing why") {
		t.Fatal(p)
	}
	out := `1. The park on repeat | Vlad went back to Regent's Park on 9 days this month, more than anywhere else on his walks.
2. Further on foot | Vlad walked about 84 km in the last 30 days, up from 51 km the month before.
3. A guess | Vlad walked 120 km because he was training for a race, which shows real dedication.
NONE`
	got := parseInsights(out, "Vlad", facts)
	if len(got) != 2 || got[0][0] != "The park on repeat" || !strings.Contains(got[1][1], "84 km") {
		t.Fatalf("%+v", got)
	}
}
