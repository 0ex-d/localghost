package pgtest

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/rates"
	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

// The minute series from the box's own minutes, the history walked back from the venues' candles
// (the USDT leg first, two venues for BTC), folded where the box was not watching, the hours rolled
// up from the minutes, the market index over all of it, and the reads.
func TestSeriesAgainstPostgres(t *testing.T) {
	db := fresh(t)
	now := time.Date(2026, 10, 2, 12, 0, 30, 0, time.UTC)
	hour := now.Truncate(time.Hour)
	// the month's weights: BTC alone, base 60000, chained from 1000
	if err := db.Exec("INSERT INTO crypto_market_weights (month, symbol, rank, weight, avg_volume_usd, base_price) VALUES ('2026-10','BTC',1,1,1e10,60000)"); err != nil {
		t.Fatal(err)
	}
	// the markets the venues were seen quoting (the tickers' quotes)
	mk := func(ex, base, quote string) rates.Market { return rates.Market{Exchange: ex, Base: base, Quote: quote} }
	for _, m := range []rates.Market{mk("binance", "BTC", "USDT"), mk("coinbase", "BTC", "USD"), mk("okx", "BTC", "USDT"), mk("coinbase", "USDT", "USD"), mk("kraken", "USDT", "USD")} {
		if err := db.Exec("INSERT INTO crypto_quotes (ts, exchange, base, quote, price, volume, quote_ts) VALUES ($1,$2,$3,$4,1,1,$1)", now.Unix(), m.Exchange, m.Base, m.Quote); err != nil {
			t.Fatal(err)
		}
	}
	seen := tally.MarketsSeen(db, now)
	if len(seen) != 5 {
		t.Fatalf("seen: %v", seen)
	}

	// 1. the hours to walk back: the USDT leg first (coinbase, kraken), then BTC on binance and coinbase
	pages := tally.NextBarPages(db, tally.ResHour, []string{"BTC"}, seen, now, 10)
	var ids []string
	for _, p := range pages {
		ids = append(ids, p.Market.ID())
	}
	if strings.Join(ids, " ") != "coinbase:USDT-USD kraken:USDT-USD binance:BTC-USDT coinbase:BTC-USD" {
		t.Fatalf("pages: %v", ids)
	}
	bn := pages[2]
	if !bn.To.Equal(hour) || bn.Oldest != 0 || bn.Floor != hour.Add(-30*24*time.Hour).Unix() || !strings.Contains(bn.URL(), "interval=1h") {
		t.Fatalf("binance page: %+v %s", bn, bn.URL())
	}
	// the USDT leg's three hours at 0.999, then binance's BTC hours in USDT
	h := func(k int) int64 { return hour.Add(time.Duration(-k) * time.Hour).Unix() }
	usdt := fmt.Sprintf(`[[%d,0.998,1.0,0.999,0.999,1000],[%d,0.998,1.0,0.999,0.999,1000],[%d,0.998,1.0,0.999,0.999,1000]]`, h(3), h(2), h(1))
	if n, older, err := tally.IngestBars(db, pages[0], []byte(usdt)); err != nil || n != 3 || !older {
		t.Fatalf("usdt page: %d %v %v", n, older, err)
	}
	btc := fmt.Sprintf(`[[%d,"61000","61500","60500","61200","10",0,"0",1,"0","0","0"],[%d,"61200","62000","61000","61800","10",0,"0",1,"0","0","0"],[%d,"61800","62500","61500","62400","10",0,"0",1,"0","0","0"]]`,
		h(3)*1000, h(2)*1000, h(1)*1000)
	if n, older, err := tally.IngestBars(db, bn, []byte(btc)); err != nil || n != 3 || !older {
		t.Fatalf("binance page: %d %v %v", n, older, err)
	}
	pts, err := tally.Series(db, "BTC", tally.ResHour, now.Add(-4*time.Hour), now)
	if err != nil || len(pts) != 3 || pts[2].Source != "venues" || math.Abs(pts[2].Close-62400*0.999) > 1e-6 || pts[0].TS != h(3) {
		t.Fatalf("folded hours: %+v %v", pts, err)
	}
	// the next page for binance walks back from the oldest hour held; nothing older comes: done
	pages = tally.NextBarPages(db, tally.ResHour, []string{"BTC"}, seen, now, 10)
	var again tally.BarPage
	for _, p := range pages {
		if p.Market.Exchange == "binance" {
			again = p
		}
	}
	if again.Oldest != h(3) || !again.To.Equal(time.Unix(h(4), 0)) {
		t.Fatalf("walk back: %+v", again)
	}
	if _, older, err := tally.IngestBars(db, again, []byte(btc)); err != nil || older {
		t.Fatalf("the same bars again are not older: %v %v", older, err)
	}
	tally.MarkBarsDone(db, tally.ResHour, again.Market)
	for _, p := range tally.NextBarPages(db, tally.ResHour, []string{"BTC"}, seen, now, 10) {
		if p.Market.Exchange == "binance" {
			t.Fatal("a done market came back")
		}
	}
	// the minute history goes to the venues that can walk a week back (not kraken)
	for _, p := range tally.NextBarPages(db, tally.ResMinute, []string{"BTC"}, seen, now, 10) {
		if p.Market.Exchange == "kraken" || !strings.Contains(p.URL(), "1m") && !strings.Contains(p.URL(), "granularity=60") {
			t.Fatalf("minute page: %s %s", p.Market.ID(), p.URL())
		}
	}

	// 2. the box's own minutes: forty minutes of the last hour, live; a folded minute gives way
	if err := db.Exec("INSERT INTO crypto_series (res, ts, symbol, open, high, low, close, n, source) VALUES ('1m',$1,'BTC',1,1,1,1,1,'venues')", h(1)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		m := time.Unix(h(1), 0).Add(time.Duration(i) * time.Minute)
		v, err := tally.RecordMinute(db, m, map[string]rates.Index{"BTC": {Price: 62000 + float64(i), N: 5}})
		if err != nil || math.Abs(v-1000*(62000+float64(i))/60000) > 1e-6 {
			t.Fatalf("minute %d: %v %v", i, v, err)
		}
	}
	mins, _ := tally.Series(db, "BTC", tally.ResMinute, time.Unix(h(1), 0), now)
	if len(mins) != 40 || mins[0].Source != "live" || mins[0].Close != 62000 || mins[0].N != 5 {
		t.Fatalf("minutes: %d %+v", len(mins), mins[0])
	}
	// 3. the hour rolled up from the minutes replaces the folded hour
	if err := tally.RollupHours(db, now); err != nil {
		t.Fatal(err)
	}
	pts, _ = tally.Series(db, "BTC", tally.ResHour, time.Unix(h(1), 0), time.Unix(h(1), 0))
	if len(pts) != 1 || pts[0].Source != "minutes" || pts[0].Open != 62000 || pts[0].Close != 62039 || pts[0].High != 62039 || pts[0].Low != 62000 || pts[0].N != 40 {
		t.Fatalf("rolled hour: %+v", pts)
	}
	ms, _ := tally.Series(db, rates.MarketIndexCode, tally.ResHour, time.Unix(h(1), 0), time.Unix(h(1), 0))
	if len(ms) != 1 || ms[0].Source != "minutes" || math.Abs(ms[0].Close-1000*62039.0/60000) > 1e-6 {
		t.Fatalf("market hour: %+v", ms)
	}
	// 4. the market over the folded hours, without touching the rolled one
	// three points offered; the rolled hour keeps its own
	n, err := tally.RefoldMarket(db, tally.ResHour, now.Add(-30*24*time.Hour), now)
	if err != nil || n != 3 {
		t.Fatalf("refold: %d %v", n, err)
	}
	ms, _ = tally.Series(db, rates.MarketIndexCode, tally.ResHour, now.Add(-4*time.Hour), now)
	if len(ms) != 3 || ms[0].Source != "venues" || math.Abs(ms[0].Close-1000*61200*0.999/60000) > 1e-6 || ms[2].Source != "minutes" {
		t.Fatalf("market hours: %+v", ms)
	}
	// 5. depth, and what is kept
	d := tally.Depth(db, []string{"BTC"}, now)
	if len(d) != 2 || d[0].Res != "1m" || d[0].Points != 40 || d[1].Points != 3 {
		t.Fatalf("depth: %+v", d)
	}
	tally.Prune(db, now.Add(40*24*time.Hour))
	if pts, _ := tally.Series(db, "BTC", tally.ResHour, now.Add(-30*24*time.Hour), now); len(pts) != 0 {
		t.Fatalf("pruned: %d", len(pts))
	}
	if _, err := tally.Series(db, "BTC", "5m", now, now); err == nil {
		t.Fatal("a resolution the box does not keep")
	}
}
