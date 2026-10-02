package hw

import (
	"testing"
	"time"
)

func TestApplyFast(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	s := RatesSnapshot{Index: map[string]IndexRow{
		"BTC": {Price: 100_000, At: now.Unix() - 50, Change24: 2, HasChange: true},
		"ETH": {Price: 4_000, At: now.Unix() - 50},
		"SOL": {Price: 200, At: now.Unix() - 50, Change24: -1, HasChange: true},
	}}
	f := Fast{At: now.UnixMilli(), Prices: map[string]FastPrice{
		"BTC": {Price: 102_000, At: now.UnixMilli() - 2000},
		"ETH": {Price: 4_010, At: now.UnixMilli() - 60_000}, // too old
		"SOL": {Price: 260, At: now.UnixMilli() - 1000},     // 30% off the minute: a bad print
		"XRP": {Price: 3, At: now.UnixMilli()},              // not in the index
	}}
	if n := ApplyFast(&s, f, now); n != 1 {
		t.Fatalf("moved %d", n)
	}
	b := s.Index["BTC"]
	// the same price a day ago (100000/1.02) against the trade: +4.04%
	if !b.Fast || b.Price != 102_000 || b.At != now.Unix()-2 || s.BTCUSD != 102_000 || b.Change24 < 4.03 || b.Change24 > 4.05 {
		t.Fatalf("%+v %v", b, s.BTCUSD)
	}
	if s.Index["ETH"].Fast || s.Index["ETH"].Price != 4_000 || s.Index["SOL"].Price != 200 {
		t.Fatalf("%+v", s.Index)
	}
}

func TestNewsDocWithin(t *testing.T) {
	d := NewsDoc{Since: 100, Stories: []NewsStory{{ID: 1, LastSeen: 150}, {ID: 2, LastSeen: 300}}}
	if got := d.Within(200); len(got.Stories) != 1 || got.Stories[0].ID != 2 || got.Since != 200 {
		t.Fatalf("%+v", got)
	}
	if got := d.Within(50); len(got.Stories) != 2 {
		t.Fatal("a since before the copy's is the whole copy (the handler goes to Postgres for it)")
	}
}
