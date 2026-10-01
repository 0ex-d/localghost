package pgtest

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/rates"
	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

// Coin days for the last days of September and the first of October, the venues' closes for BTC
// only: September's weights come from its own first day (nothing before it), October's from
// September's average volume; BTC is priced from the venues, the rest from the rank list; a coin
// with no row on a day is carried; the live value follows the live index.
func TestMarketIndexAgainstPostgres(t *testing.T) {
	db := fresh(t)
	ins := func(day, sym string, rank int, price, cap, vol float64) {
		if err := db.Exec("INSERT INTO coin_daily (day, symbol, coin_id, rank, price_usd, market_cap, volume_usd, source) VALUES ($1,$2,$2,$3,$4,$5,$6,'test')", day, sym, rank, price, cap, vol); err != nil {
			t.Fatal(err)
		}
	}
	// 28–30 September: BTC, ETH, SOL, USDT (never in), WBTC (never in)
	for d, p := range map[string]float64{"2026-09-28": 60000, "2026-09-29": 61000, "2026-09-30": 62000} {
		ins(d, "BTC", 1, p, 1.2e12, 3e10)
		ins(d, "ETH", 2, 3000, 3.6e11, 1.5e10)
		ins(d, "USDT", 3, 1, 1.7e11, 1e11)
		ins(d, "SOL", 4, 150, 7e10, 3e9)
		ins(d, "WBTC", 5, p, 1e10, 1e9)
	}
	// the venues' close for BTC on the 30th (the base) and 1 October
	for _, r := range [][]any{{"2026-09-30", 62100.0}, {"2026-10-01", 65000.0}} {
		if err := db.Exec("INSERT INTO crypto_daily_index (day, symbol, close, n) VALUES ($1,'BTC',$2,3)", r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	// 1 October: ETH up 10%, SOL has no row (carried), BTC from the venues
	ins("2026-10-01", "BTC", 1, 64900, 1.3e12, 3.2e10)
	ins("2026-10-01", "ETH", 2, 3300, 4e11, 1.6e10)
	ins("2026-10-01", "USDT", 3, 1, 1.7e11, 1e11)

	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	n, err := tally.RebuildMarketIndex(db, now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("days written: %d", n)
	}
	// September: weights from the 28th alone, base prices the 28th's, 1000 on the 28th
	sep, _ := db.Query("SELECT symbol, weight, base_price FROM crypto_market_weights WHERE month = '2026-09' ORDER BY rank")
	if len(sep.Vals) != 3 || *sep.Vals[0][0] != "BTC" || *sep.Vals[1][0] != "ETH" || *sep.Vals[2][0] != "SOL" {
		t.Fatalf("september: %v", sep.Vals)
	}
	wBTC := 3e10 / (3e10 + 1.5e10 + 3e9)
	day := func(d string) (float64, string) {
		rows, _ := db.Query("SELECT value, missing FROM crypto_market_index WHERE day = $1", d)
		if len(rows.Vals) != 1 {
			t.Fatalf("no value for %s", d)
		}
		var v float64
		if rows.Vals[0][0] != nil {
			v = atof(*rows.Vals[0][0])
		}
		m := ""
		if rows.Vals[0][1] != nil {
			m = *rows.Vals[0][1]
		}
		return v, m
	}
	if v, _ := day("2026-09-28"); math.Abs(v-1000) > 1e-9 {
		t.Fatalf("28th: %v", v)
	}
	// 29th: BTC 61000/60000, the rest flat
	if v, _ := day("2026-09-29"); math.Abs(v-1000*(1+wBTC*(61000.0/60000-1))) > 1e-6 {
		t.Fatalf("29th: %v", v)
	}
	// 30th: BTC from the venues (62100), not the rank list's 62000
	v30, _ := day("2026-09-30")
	if math.Abs(v30-1000*(1+wBTC*(62100.0/60000-1))) > 1e-6 {
		t.Fatalf("30th: %v", v30)
	}
	// October: weights from September's average volume (the same three), base = the 30th's closes
	oct, _ := db.Query("SELECT symbol, base_price FROM crypto_market_weights WHERE month = '2026-10' ORDER BY rank")
	if len(oct.Vals) != 3 || *oct.Vals[0][0] != "BTC" || atof(*oct.Vals[0][1]) != 62100 || atof(*oct.Vals[1][1]) != 3000 {
		t.Fatalf("october: %v", oct.Vals)
	}
	// 1 October: BTC 65000/62100, ETH 3300/3000, SOL carried at 150: chain from the 30th
	wETH := 1.5e10 / (3e10 + 1.5e10 + 3e9)
	want := v30 * (wBTC*(65000.0/62100) + wETH*1.1 + (1-wBTC-wETH)*1)
	v1, missing := day("2026-10-01")
	if math.Abs(v1-want) > 1e-6 || missing != "SOL" {
		t.Fatalf("1 oct: %v %q (want %v)", v1, missing, want)
	}
	// a second rebuild starts two days back and changes nothing
	if n, err := tally.RebuildMarketIndex(db, now); err != nil || n != 3 {
		t.Fatalf("again: %d %v", n, err)
	}
	if v, _ := day("2026-10-01"); math.Abs(v-v1) > 1e-9 {
		t.Fatal("rebuild moved the value")
	}
	// live: BTC's index 66000 now, ETH and SOL from the newest coin day
	if err := db.Exec("INSERT INTO crypto_index (ts, symbol, price, n, spread, used, dropped) VALUES ($1,'BTC',66000,4,0.001,'a,b,c,d','{}')", now.Unix()); err != nil {
		t.Fatal(err)
	}
	st, err := tally.MarketNow(db, now)
	if err != nil {
		t.Fatal(err)
	}
	wantNow := v30 * (wBTC*(66000.0/62100) + wETH*1.1 + (1-wBTC-wETH)*1)
	if st.Code != rates.MarketIndexCode || st.Month != "2026-10" || st.Constituents != 3 || st.Priced != 1 || math.Abs(st.Value-wantNow) > 1e-6 || st.Day != "2026-10-01" || st.Days != 4 {
		t.Fatalf("now: %+v (want %v)", st, wantNow)
	}
	if math.Abs(st.DayChange-100*(wantNow/v1-1)) > 1e-9 || len(st.Top) != 3 || st.Top[0].Symbol != "BTC" {
		t.Fatalf("change/top: %+v", st)
	}
	hist, err := tally.MarketHistory(db, 10)
	if err != nil || len(hist) != 4 || hist[0].Day != "2026-10-01" {
		t.Fatalf("history: %v %v", hist, err)
	}
	// the symbols followed: the fifty largest of the newest day, stablecoins and wrapped out, plus by hand
	syms := tally.Symbols(db)
	if len(syms) != 2 || syms[0] != "BTC" || syms[1] != "ETH" {
		t.Fatalf("symbols: %v", syms)
	}
	_ = tally.SetSymbols(db, []string{"SOL"})
	if syms := tally.Symbols(db); len(syms) != 3 || syms[2] != "SOL" {
		t.Fatalf("symbols with a manual one: %v", syms)
	}
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
