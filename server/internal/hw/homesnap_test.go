package hw

import (
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

func TestMakeHomeSnap(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	rd := &RatesDoc{RatesSnapshot: RatesSnapshot{Index: map[string]IndexRow{
		"BTC": {Price: 100_000, At: now.Unix() - 50, Change24: 2, HasChange: true, N: 6},
		"ETH": {Price: 4_000, At: now.Unix() - 50},
		"XRP": {Price: 3, At: now.Unix() - 50},
	}}, Market: &tally.MarketState{Code: "CRYPTO50", Value: 1234.5, DayChange: -1.5}}
	f := &Fast{At: now.UnixMilli(), Prices: map[string]FastPrice{
		"BTC": {Price: 101_000, At: now.UnixMilli() - 1000, N: 5},
	}}
	nd := &NewsDoc{Brief: "- Rates held.", BriefAt: now.Unix() - 600, BriefStories: []int64{7}, Stories: []NewsStory{
		{ID: 1, Title: "old", Sources: 9, LastSeen: now.Unix() - 2*86400},
		{ID: 2, Title: "two outlets", Sources: 2, LastSeen: now.Unix() - 100, Summary: "The lead.\n- a point",
			Items: []NewsItem{{Outlet: "BBC"}, {Outlet: "BBC"}, {Outlet: "Reuters"}}},
		{ID: 3, Title: "five outlets", Sources: 5, LastSeen: now.Unix() - 3000},
		{ID: 4, Title: "", Sources: 9, LastSeen: now.Unix()},
		{ID: 5, Title: "two, newer", Sources: 2, LastSeen: now.Unix() - 10},
	}}
	s := MakeHomeSnap(rd, f, nd, now)
	if len(s.Prices) != 2 || s.Prices["BTC"].Price != 101_000 || s.Prices["BTC"].N != 5 || s.Prices["ETH"].Price != 4_000 {
		t.Fatalf("prices %+v", s.Prices)
	}
	if _, ok := s.Prices["XRP"]; ok {
		t.Fatal("only home's coins ride along")
	}
	if rd.Index["BTC"].Price != 100_000 {
		t.Fatal("the fast lane went over the doc itself, not a copy")
	}
	if s.MarketCode != "CRYPTO50" || s.MarketValue != 1234.5 || s.MarketChange != -1.5 {
		t.Fatalf("market %+v", s)
	}
	if s.Brief != "- Rates held." || len(s.BriefStories) != 1 {
		t.Fatalf("brief %+v", s)
	}
	if len(s.Top) != 3 || s.Top[0].ID != 3 || s.Top[1].ID != 5 || s.Top[2].ID != 2 {
		t.Fatalf("top %+v", s.Top)
	}
	if s.Top[2].Lead != "The lead." || len(s.Top[2].Outlets) != 2 {
		t.Fatalf("story %+v", s.Top[2])
	}
	empty := MakeHomeSnap(nil, nil, nil, now)
	if empty.Prices == nil || empty.Top == nil || empty.BriefStories == nil {
		t.Fatal("an empty snapshot says so with empty lists, not nulls")
	}
}

func TestAboutHashCarriesTheVersion(t *testing.T) {
	if AboutHash(" I am Vlad. ") != AboutHash("I am Vlad.") {
		t.Fatal("spaces around the note are not a change")
	}
	if len(AboutHash("x")) != 16 || AboutHash("x") == AboutHash("y") {
		t.Fatal(AboutHash("x"))
	}
}
