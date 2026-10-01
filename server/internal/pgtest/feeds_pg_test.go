package pgtest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/feedstat"
	"github.com/LocalGhostDao/localghost/server/internal/monitor"
	"github.com/LocalGhostDao/localghost/server/internal/rates"
	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

// The fetch log read back per exchange and per source: success over the window, the latencies,
// the run of failures since the last good fetch (a minute's calls count once), the newest error.
func TestFetchLogStats(t *testing.T) {
	db := fresh(t)
	now := time.Date(2026, 10, 1, 12, 0, 2, 0, time.UTC)
	t0, t1 := now.Add(-2*time.Minute), now.Add(-time.Minute)
	logAt := func(at time.Time, es ...feedstat.Entry) {
		t.Helper()
		if err := feedstat.Log(db, at, es); err != nil {
			t.Fatal(err)
		}
	}
	tk := feedstat.KindTicker
	logAt(t0,
		feedstat.Entry{Source: "binance:all", Kind: tk, By: "box", Status: 200, OK: true, TookMs: 100, Items: 50},
		feedstat.Entry{Source: "coinbase:BTC-USD", Kind: tk, By: "box", Status: 200, OK: true, TookMs: 200, Items: 1},
		feedstat.Entry{Source: "coinbase:BNB-USD", Kind: tk, By: "box", Status: 404, TookMs: 50, Error: "HTTP 404"},
		feedstat.Entry{Source: "okx:all", Kind: tk, By: "box", Status: 429, TookMs: 80, Error: "HTTP 429"})
	logAt(t1,
		feedstat.Entry{Source: "binance:all", Kind: tk, By: "box", Status: 200, OK: true, TookMs: 300, Items: 50},
		feedstat.Entry{Source: "coinbase:BTC-USD", Kind: tk, By: "box", Status: 200, OK: true, TookMs: 220, Items: 1},
		feedstat.Entry{Source: "okx:all", Kind: tk, By: "box", Status: 429, TookMs: 90, Error: "HTTP 429"})
	logAt(now,
		feedstat.Entry{Source: "binance:all", Kind: tk, By: "box", TookMs: 5000, Error: "timeout"},
		feedstat.Entry{Source: "okx:all", Kind: tk, By: "box", Status: 200, OK: true, TookMs: 900, Items: 48})
	// an old row past the window counts for the run of failures, not for the window
	logAt(now.Add(-3*time.Hour), feedstat.Entry{Source: "binance:all", Kind: tk, By: "box", Status: 200, OK: true, TookMs: 100, Items: 50})

	st, err := feedstat.Stats(db, tk, now.Add(-time.Hour), true)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]feedstat.Stat{}
	for _, s := range st {
		by[s.Key] = s
	}
	b := by["binance"]
	if b.Calls != 3 || b.OK != 2 || b.FailsInRow != 1 || b.LastOKAt != t1.Unix() || b.LastAt != now.Unix() || b.LastError != "timeout" || b.MaxMs != 5000 || b.P50Ms != 300 || b.LastOK {
		t.Fatalf("binance: %+v", b)
	}
	o := by["okx"]
	if o.Calls != 3 || o.OK != 1 || o.FailsInRow != 0 || !o.LastOK || o.LastItems != 48 || o.LastError != "HTTP 429" || o.LastErrorAt != t1.Unix() {
		t.Fatalf("okx: %+v", o)
	}
	c := by["coinbase"]
	if c.Calls != 3 || c.OK != 2 || c.FailsInRow != 0 || c.LastItems != 1 || c.LastErrorAt != t0.Unix() || c.LastBy != "box" {
		t.Fatalf("coinbase: %+v", c)
	}
	st, _ = feedstat.Stats(db, tk, now.Add(-time.Hour), false)
	if len(st) != 4 {
		t.Fatalf("by source: %+v", st)
	}
	for _, s := range st {
		if s.Key == "coinbase:BNB-USD" && (s.FailsInRow != 1 || s.LastOKAt != 0) {
			t.Fatalf("bnb: %+v", s)
		}
	}
	if n := feedstat.Count(db, tk, now.Add(-90*time.Second)); n != 5 {
		t.Fatalf("count: %d", n)
	}
	feedstat.Prune(db, now.Add(feedstat.Keep).Add(-time.Hour)) // the three-hour-old row goes
	if n := feedstat.Count(db, tk, time.Unix(0, 0)); n != 9 {
		t.Fatalf("after prune: %d", n)
	}
}

// A rates batch leaves a row per source: the good with what they gave, the failed with why.
func TestIngestRatesLogsEachSource(t *testing.T) {
	db := fresh(t)
	now := time.Date(2026, 10, 1, 12, 0, 2, 0, time.UTC)
	body := fmt.Sprintf(`[{"symbol":"BTCUSDT","lastPrice":"65000","volume":"10","closeTime":%d},{"symbol":"ETHUSDT","lastPrice":"3200","volume":"100","closeTime":%d}]`, now.UnixMilli(), now.UnixMilli())
	batch := fmt.Sprintf(`{"fetchedAt":%d,"by":"box","sources":[{"id":"binance:all","status":200,"body":%q,"tookMs":140},{"id":"kraken:all","status":503,"tookMs":2000},{"id":"coinpaprika","status":0,"error":"dial tcp: i/o timeout"}]}`, now.Unix(), body)
	if _, err := tally.IngestRates(db, []byte(batch), now); err != nil {
		t.Fatal(err)
	}
	st, err := feedstat.Stats(db, feedstat.KindTicker, now.Add(-time.Hour), false)
	if err != nil || len(st) != 2 {
		t.Fatalf("tickers: %+v %v", st, err)
	}
	for _, s := range st {
		switch s.Key {
		case "binance:all":
			if !s.LastOK || s.LastItems != 2 || s.P50Ms != 140 || s.Bytes != int64(len(body)) {
				t.Fatalf("binance: %+v", s)
			}
		case "kraken:all":
			if s.LastOK || s.LastError != "HTTP 503" || s.FailsInRow != 1 {
				t.Fatalf("kraken: %+v", s)
			}
		}
	}
	rk, _ := feedstat.Stats(db, feedstat.KindRanks, now.Add(-time.Hour), false)
	if len(rk) != 1 || rk[0].Key != "coinpaprika" || !strings.Contains(rk[0].LastError, "timeout") {
		t.Fatalf("ranks: %+v", rk)
	}
}

// The report over a box a few hours in: the minute ticking, two exchanges answering and one not,
// the history half walked, an ECB table due and missing, a rank list, three feeds with one failing.
func TestMonitorReport(t *testing.T) {
	db := fresh(t)
	now := time.Date(2026, 10, 1, 15, 0, 2, 0, time.UTC) // Thursday, 17:00 in Frankfurt
	minute := now.Truncate(time.Minute)
	exec := func(q string, args ...any) {
		t.Helper()
		if err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// the phone on Wi-Fi four minutes ago
	exec("INSERT INTO settings (key, value) VALUES ('phone_net','wifi'), ('phone_seen',$1)", fmt.Sprint(now.Unix()-240))
	// two hours of minutes for BTC, four missing in the last hour, and the newest index
	for k := 1; k <= 120; k++ {
		if k >= 30 && k <= 33 {
			continue
		}
		exec("INSERT INTO crypto_series (res, ts, symbol, open, high, low, close, n, source) VALUES ('1m',$1,'BTC',1,1,1,1,3,'live')", minute.Add(time.Duration(-k)*time.Minute).Unix())
	}
	exec("INSERT INTO crypto_series (res, ts, symbol, open, high, low, close, n, source) VALUES ('1m',$1,'BTC',1,1,1,1,3,'live')", minute.Unix())
	exec(`INSERT INTO crypto_index (ts, symbol, price, n, spread, used, dropped) VALUES ($1,'BTC',65000,2,0.001,'binance,coinbase','{"gemini":"no volume given"}'),
		($1,'ETH',3200,1,0,'binance','{"okx":"stale (20m0s old)"}')`, now.Unix())
	exec(`INSERT INTO crypto_quotes (ts, exchange, base, quote, price, volume, quote_ts) VALUES ($1,'binance','BTC','USDT',65000,10,$2), ($1,'coinbase','BTC','USD',65010,5,$3), ($1,'binance','ETH','USDT',3200,100,$2)`,
		now.Unix(), now.Unix()-3, now.Unix())
	tk := feedstat.KindTicker
	for k := 10; k >= 0; k-- {
		at := now.Add(time.Duration(-k) * time.Minute)
		es := []feedstat.Entry{
			{Source: "binance:all", Kind: tk, By: "box", Status: 200, OK: true, TookMs: 150, Items: 2},
			{Source: "coinbase:BTC-USD", Kind: tk, By: "box", Status: 200, OK: true, TookMs: 250, Items: 1},
			{Source: "okx:all", Kind: tk, By: "box", Status: 429, TookMs: 90, Error: "HTTP 429"},
			{Source: "tallyd.minute", Kind: feedstat.KindTick, By: "box", OK: true, TookMs: 3000, Items: 17},
		}
		if err := feedstat.Log(db, at, es); err != nil {
			t.Fatal(err)
		}
	}
	// the history: progress as tallyd keeps it, half of the minutes still walking
	if err := tally.SaveProgress(db, tally.Progress{At: now.Unix() - 30, Symbols: 2, Seen: 3, DailyFresh: 2,
		History:  []tally.HistoryState{{Res: tally.ResHour, Markets: 4, Covered: 1, Folded: true}, {Res: tally.ResMinute, Markets: 4, Walking: 2, PagesLeft: 20, Covered: 0.5}},
		Backfill: tally.BackfillState{Markets: 3, Done: 1, OldestDay: "2013-04-01"}}); err != nil {
		t.Fatal(err)
	}
	for k := 0; k < 6; k++ {
		_ = feedstat.Log(db, now.Add(time.Duration(-k)*time.Minute), []feedstat.Entry{{Source: "bars:1m:binance:BTC-USDT", Kind: feedstat.KindHistory, By: "box", Status: 200, OK: true, Items: 1000}})
	}
	// the ECB's newest is Monday's; Tuesday to Thursday are due
	exec("INSERT INTO fx_rates (day, code, rate) VALUES ('2026-09-28','USD',1.1), ('2026-09-28','GBP',0.85), ('2026-09-25','USD',1.1)")
	// a rank list half an hour old
	exec("INSERT INTO coin_ranks (ts, rank, coin_id, symbol, source) VALUES ($1,1,'bitcoin','BTC','coingecko'), ($1,2,'ethereum','ETH','coingecko')", now.Unix()-1800)
	// three feeds, one failing three times in a row
	exec(`INSERT INTO news_feeds (id, name, url, enabled, last_fetch, last_ok, last_status, failures) VALUES
		('bbc','BBC','u1',true,$1,$1,'ok',0), ('ft','FT','u2',true,$1,$1,'ok',0), ('dw','DW','u3',true,$1,$2,'HTTP 403',3)`, now.Unix()-1200, now.Unix()-86400)
	exec("INSERT INTO news_items (feed_id, guid, title, published, fetched) VALUES ('bbc','g1','One',$1,$2), ('ft','g2','Two',$3,$2)", now.Unix()-4000, now.Unix()-1200, now.Unix()-2000)
	exec("INSERT INTO settings (key, value) VALUES ('news_last_by', $1)", "phone@"+fmt.Sprint(now.Unix()-1200))

	r := monitor.Make(db, now)
	sec := map[string]monitor.Section{}
	for _, s := range r.Sections {
		sec[s.ID] = s
	}
	want := map[string]string{"fetching": monitor.OK, "prices": monitor.Flaky, "venues": monitor.Flaky, "history": monitor.Filling,
		"market": monitor.Waiting, "ecb": monitor.Late, "ranks": monitor.OK, "daily": monitor.Filling, "news": monitor.Flaky}
	for id, st := range want {
		s, ok := sec[id]
		if !ok {
			t.Fatalf("no section %s", id)
		}
		if s.State != st {
			t.Errorf("%s: %s, want %s (%s) %+v", id, s.State, st, s.Line, s.Rows)
		}
	}
	if r.State != monitor.Late || !strings.Contains(r.Summary, "ecb rates late") {
		t.Fatalf("report: %s %s", r.State, r.Summary)
	}
	if !strings.Contains(sec["fetching"].Line, "phone on Wi-Fi") || sec["fetching"].AgeS != 240 {
		t.Fatalf("fetching: %+v", sec["fetching"])
	}
	if p := sec["prices"]; p.AgeS != 0 || !strings.Contains(p.Line, "56 of 60 minutes") || !strings.Contains(p.Line, "BTC 65,000") {
		t.Fatalf("prices: %+v", p)
	}
	rows := map[string]monitor.Row{}
	for _, row := range sec["venues"].Rows {
		rows[row.K] = row
	}
	if rows["okx"].State != monitor.Failing || !strings.Contains(rows["okx"].V, "HTTP 429") || !strings.Contains(rows["okx"].V, "left out: stale") {
		t.Fatalf("okx: %+v", rows["okx"])
	}
	if rows["binance"].State != monitor.OK || !strings.Contains(rows["binance"].V, "150 ms") || !strings.Contains(rows["binance"].V, "its prices 3 s behind") || !strings.Contains(rows["binance"].V, "in the index 100%") {
		t.Fatalf("binance: %+v", rows["binance"])
	}
	if rows["gemini"].State != monitor.Waiting || !strings.Contains(rows["gemini"].V, "not asked") {
		t.Fatalf("gemini: %+v", rows["gemini"])
	}
	if !strings.Contains(sec["venues"].Line, "2 of 7 answering") {
		t.Fatalf("venues line: %s", sec["venues"].Line)
	}
	if h := sec["history"].Line; !strings.Contains(h, "hourly whole") || !strings.Contains(h, "minutes 50%") || !strings.Contains(h, "left") {
		t.Fatalf("history: %s", h)
	}
	if e := sec["ecb"].Line; !strings.Contains(e, "Mon 28 Sep") || !strings.Contains(e, "3 working days behind") {
		t.Fatalf("ecb: %s", e)
	}
	if n := sec["news"]; n.AgeS != 1200 || !strings.Contains(n.Line, "2 of 3 feeds") || !strings.Contains(n.Line, "by the phone") {
		t.Fatalf("news: %+v", n)
	}
	var dw bool
	for _, row := range sec["news"].Rows {
		if row.K == "DW" && row.State == monitor.Failing {
			dw = true
		}
	}
	if !dw {
		t.Fatalf("news rows: %+v", sec["news"].Rows)
	}
}

// The history's progress worked out from the bars held: a market not started, one half walked,
// one done; and kept for the status reads.
func TestHistoryProgress(t *testing.T) {
	db := fresh(t)
	now := time.Date(2026, 10, 2, 12, 0, 30, 0, time.UTC)
	for _, m := range []rates.Market{{Exchange: "binance", Base: "BTC", Quote: "USDT"}, {Exchange: "coinbase", Base: "BTC", Quote: "USD"}, {Exchange: "coinbase", Base: "USDT", Quote: "USD"}} {
		if err := db.Exec("INSERT INTO crypto_quotes (ts, exchange, base, quote, price, volume, quote_ts) VALUES ($1,$2,$3,$4,1,1,$1)", now.Unix(), m.Exchange, m.Base, m.Quote); err != nil {
			t.Fatal(err)
		}
	}
	seen := tally.MarketsSeen(db, now)
	hour := now.Truncate(time.Hour)
	// coinbase USDT: walked to the floor; binance BTC: fifteen days of thirty; coinbase BTC: nothing yet
	if err := db.Exec("INSERT INTO crypto_bars (res, ts, exchange, base, quote, close) VALUES ('1h',$1,'coinbase','USDT','USD',1), ('1h',$2,'binance','BTC','USDT',60000)",
		hour.Add(-31*24*time.Hour).Unix(), hour.Add(-15*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	h := tally.HistoryProgress(db, tally.ResHour, []string{"BTC"}, seen, now)
	if h.Markets != 3 || h.Walking != 2 || h.Covered < 0.49 || h.Covered > 0.51 || h.PagesLeft < 2 || h.Folded {
		t.Fatalf("progress: %+v", h)
	}
	p := tally.MakeProgress(db, []string{"BTC"}, seen, now, "binance:BTC-USDT back to 2017")
	if err := tally.SaveProgress(db, p); err != nil {
		t.Fatal(err)
	}
	back, ok := tally.LoadProgress(db)
	if !ok || back.Seen != 3 || len(back.History) != 2 || back.History[0].Res != tally.ResHour || back.Backfill.Next != "binance:BTC-USDT back to 2017" {
		t.Fatalf("kept: %+v %v", back, ok)
	}
}
