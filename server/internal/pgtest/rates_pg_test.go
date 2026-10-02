package pgtest

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"github.com/LocalGhostDao/localghost/server/internal/rates"
	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

const ecbBody = `<?xml version="1.0"?><gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref"><Cube><Cube time="2026-09-30">
<Cube currency="USD" rate="1.2"/><Cube currency="GBP" rate="0.9"/><Cube currency="RON" rate="5.0"/><Cube currency="JPY" rate="170"/><Cube currency="CHF" rate="0.95"/>
<Cube currency="SEK" rate="11"/><Cube currency="NOK" rate="11.5"/><Cube currency="DKK" rate="7.46"/><Cube currency="PLN" rate="4.3"/><Cube currency="CZK" rate="24"/><Cube currency="HUF" rate="390"/>
</Cube><Cube time="2026-09-29">
<Cube currency="USD" rate="1.19"/><Cube currency="GBP" rate="0.89"/><Cube currency="RON" rate="5.0"/><Cube currency="JPY" rate="170"/><Cube currency="CHF" rate="0.95"/>
<Cube currency="SEK" rate="11"/><Cube currency="NOK" rate="11.5"/><Cube currency="DKK" rate="7.46"/><Cube currency="PLN" rate="4.3"/><Cube currency="CZK" rate="24"/><Cube currency="HUF" rate="390"/>
</Cube></Cube></gesmes:Envelope>`

// A batch with two ECB days, BTC on four venues (one in USDT, one rate-limited), ETH on one, the
// USDT/USD legs, a week of candles from two venues and the rank list; then a thin batch an hour
// later. The snapshot, the daily index, the marks and the converter say it all.
func TestRatesIngestAgainstPostgres(t *testing.T) {
	db := fresh(t)
	at := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	ms := func(t time.Time) string { return itoa(int(t.UnixMilli())) }
	day := func(d int) time.Time { return time.Date(2026, 9, 24+d, 0, 0, 0, 0, time.UTC) }
	coins := `{"data":[{"slug":"bitcoin","symbol":"BTC","name":"Bitcoin","listed":true,"rank":1,"latest":"65000","market_cap":"1.28e12","volume_24h":"3e10","percent_change":0.012,"circulating_supply":"19700000"}`
	for i := 2; i <= 12; i++ {
		coins += `,{"slug":"c` + itoa(i) + `","symbol":"s","name":"Coin","listed":true,"rank":` + itoa(i) + `,"latest":"10","market_cap":"1","volume_24h":"1","percent_change":0}`
	}
	coins += `]}`
	// candles: coinbase BTC-USD and binance BTC-USDT for seven days, and the USDT/USD leg on coinbase
	cb, bn, us := `[`, `[`, `[`
	for d := 0; d < 7; d++ {
		if d > 0 {
			cb += ","
			bn += ","
			us += ","
		}
		p := 60000 + 100*d
		cb += `[` + itoa(int(day(d).Unix())) + `,` + itoa(p-500) + `,` + itoa(p+500) + `,` + itoa(p) + `,` + itoa(p) + `,100]`
		bn += `[` + ms(day(d)) + `,"` + itoa(p) + `","` + itoa(p+500) + `","` + itoa(p-500) + `","` + itoa(p+100) + `","200",` + ms(day(d).Add(24*time.Hour-time.Millisecond)) + `,"0",1,"0","0","0"]`
		us += `[` + itoa(int(day(d).Unix())) + `,0.998,1.001,0.999,0.999,1000]`
	}
	cb += `]`
	bn += `]`
	us += `]`
	batch := map[string]any{"fetchedAt": at.Unix(), "sources": []map[string]any{
		{"id": "ecb", "status": 200, "body": ecbBody},
		{"id": "coinbase:BTC-USD", "status": 200, "body": `{"price":"65000","volume":"10000","time":"` + at.Add(-30*time.Second).Format(time.RFC3339) + `"}`},
		{"id": "kraken:BTC-USD", "status": 200, "body": `{"result":{"XXBTZUSD":{"c":["65100","1"],"v":["1","2000"]}}}`},
		{"id": "bitstamp:BTC-USD", "status": 429, "body": `{"error":"too many"}`},
		{"id": "binance:BTC-USDT", "status": 200, "body": `{"lastPrice":"65130","volume":"4000","closeTime":` + ms(at) + `}`},
		{"id": "coinbase:ETH-USD", "status": 200, "body": `{"price":"3500","volume":"50000","time":"` + at.Format(time.RFC3339) + `"}`},
		{"id": "coinbase:USDT-USD", "status": 200, "body": `{"price":"0.9990","volume":"1e6","time":"` + at.Format(time.RFC3339) + `"}`},
		{"id": "kraken:USDT-USD", "status": 200, "body": `{"result":{"USDTZUSD":{"c":["0.9992","1"],"v":["1","1e6"]}}}`},
		{"id": "hist:coinbase:BTC-USD", "status": 200, "body": cb},
		{"id": "hist:binance:BTC-USDT", "status": 200, "body": bn},
		{"id": "hist:coinbase:USDT-USD", "status": 200, "body": us},
		{"id": "coinbase-ranks", "status": 200, "body": coins},
	}}
	raw, _ := json.Marshal(batch)
	res, err := tally.IngestRates(db, raw, at)
	if err != nil {
		t.Fatal(err)
	}
	if res.FXDay != "2026-09-30" || res.FXDays != 2 || res.Quotes != 6 || res.Coins != 12 || res.CoinSource != "coinbase-ranks" || res.Failed["bitstamp:BTC-USD"] != "HTTP 429" {
		t.Fatalf("%+v", res)
	}
	usdt := 0.9991
	binanceUSD := 65130 * usdt
	want := (65000*10000 + 65100*2000 + binanceUSD*4000) / 16000.0
	if ix := res.Index["BTC"]; ix.N != 3 || math.Abs(ix.Price-want) > 0.01 {
		t.Fatalf("btc index %+v want %.2f", ix, want)
	}
	if ix := res.Index["ETH"]; ix.N != 1 || ix.Price != 3500 {
		t.Fatalf("eth index %+v", ix)
	}
	if ix := res.Index["USDT"]; ix.N != 2 || math.Abs(ix.Price-usdt) > 1e-6 {
		t.Fatalf("usdt index %+v", ix)
	}
	if res.Candles != 21 || res.DaysIndex != 14 { // 7 days BTC + 7 days USDT indexed
		t.Fatalf("candles %d days %d", res.Candles, res.DaysIndex)
	}
	snap, err := hw.RatesNow(db)
	if err != nil || snap.FXDay != "2026-09-30" || snap.FX["GBP"] != 0.9 || snap.FXDays != 2 || snap.BTCN != 3 || snap.BTCUsed != "binance,coinbase,kraken" || len(snap.Ranks) != 12 || snap.Days != 7 {
		t.Fatalf("snapshot: %+v %v", snap, err)
	}
	if v, err := rates.Convert(100, "EUR", "GBP", snap.FX, snap.USD()); err != nil || math.Abs(v-90) > 1e-9 {
		t.Fatalf("convert: %v %v", v, err)
	}
	if v, err := rates.Convert(1, "ETH", "USD", snap.FX, snap.USD()); err != nil || v != 3500 {
		t.Fatalf("eth→usd: %v %v", v, err)
	}
	// the daily index: coinbase 60600 and binance (60700 * 0.999) on day 6
	hist, err := hw.RatesHistory(db, "btc", 10)
	if err != nil || len(hist) != 7 || hist[0].Day != "2026-09-30" || hist[0].N != 2 || math.Abs(hist[0].Close-(60600+60700*0.999)/2) > 1e-6 {
		t.Fatalf("history: %+v %v", hist, err)
	}
	if fx, _ := hw.RatesHistory(db, "GBP", 10); len(fx) != 2 || fx[0].Close != 0.9 || fx[1].Close != 0.89 {
		t.Fatalf("fx history: %+v", fx)
	}
	marks := tally.Marks(db)
	if marks["coinbase:BTC-USD"] != at.Unix() || marks["bitstamp:BTC-USD"] != at.Unix() || marks["coinbase-ranks"] != at.Unix() || marks["hist:coinbase:BTC-USD"] != at.Unix() {
		t.Fatalf("marks: %v", marks)
	}
	if o := tally.OldestDay(db, rates.Market{Exchange: "coinbase", Base: "BTC", Quote: "USD"}); o != "2026-09-24" {
		t.Fatalf("oldest: %s", o)
	}
	if n := tally.NewestDay(db, rates.Market{Exchange: "binance", Base: "BTC", Quote: "USDT"}); n != "2026-09-30" {
		t.Fatalf("newest: %s", n)
	}
	// an hour on, kraken alone: coinbase's quote is an hour old now, the index is kraken alone
	at2 := at.Add(time.Hour)
	batch2 := map[string]any{"fetchedAt": at2.Unix(), "sources": []map[string]any{
		{"id": "kraken:BTC-USD", "status": 200, "body": `{"result":{"XXBTZUSD":{"c":["66000","1"],"v":["1","2000"]}}}`},
		{"id": "coinbase-ranks", "status": 200, "body": `{"data":[]}`},
	}}
	raw, _ = json.Marshal(batch2)
	res, err = tally.IngestRates(db, raw, at2)
	if err != nil || res.Index["BTC"].N != 1 || res.Failed["coinbase-ranks"] == "" {
		t.Fatalf("second: %+v %v", res, err)
	}
	// ten minutes on, bitstamp alone: kraken's ten-minute-old quote joins it
	at3 := at2.Add(10 * time.Minute)
	batch3 := map[string]any{"fetchedAt": at3.Unix(), "sources": []map[string]any{
		{"id": "bitstamp:BTC-USD", "status": 200, "body": `{"timestamp":"` + itoa(int(at3.Unix())) + `","last":"66100","volume":"1000"}`},
	}}
	raw, _ = json.Marshal(batch3)
	res, err = tally.IngestRates(db, raw, at3)
	if err != nil || res.Index["BTC"].N != 2 {
		t.Fatalf("third: %+v %v", res, err)
	}
	snap, _ = hw.RatesNow(db)
	if snap.BTCAt != at3.Unix() || snap.Index["ETH"].At != at.Unix() {
		t.Fatalf("newest: %+v", snap)
	}
	// the symbols followed: the rank list's (the fixture's coins are BTC and eleven "S"), then by hand
	if syms := tally.Symbols(db); len(syms) != 2 || syms[0] != "BTC" || syms[1] != "S" {
		t.Fatalf("symbols: %v", syms)
	}
	if err := tally.SetSymbols(db, []string{"ETH", "SOL"}); err != nil {
		t.Fatal(err)
	}
	if syms := tally.Symbols(db); len(syms) != 4 || syms[2] != "ETH" || syms[3] != "SOL" {
		t.Fatalf("symbols: %v", syms)
	}
	// a coin day per ranked coin landed with the rank list
	if rows, _ := db.Query("SELECT count(*) FROM coin_daily WHERE day = '2026-10-01'"); *rows.Vals[0][0] != "2" {
		t.Fatalf("coin days: %v", rows.Vals)
	}
	if _, ok := tally.ParseRates([]byte(`{"sources":[]}`)); ok {
		t.Fatal("an empty batch passed")
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// The backfill walks a market back from its oldest candle one venue page at a time, skips what
// is done, and stops at the floor.
func TestBackfillWalksBack(t *testing.T) {
	db := fresh(t)
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	mk := rates.Markets([]string{"BTC"})
	if m, _, _, why := tally.NextBackfill(db, mk, now); m != nil || why == "" {
		t.Fatalf("nothing held yet: %v %s", m, why)
	}
	// a week of coinbase BTC-USD candles
	for d := 24; d <= 30; d++ {
		if err := db.Exec("INSERT INTO crypto_daily (day, symbol, exchange, quote, open, high, low, close, volume) VALUES ($1,'BTC','coinbase','USD',1,1,1,60000,1)", "2026-09-"+itoa(d)); err != nil {
			t.Fatal(err)
		}
	}
	m, from, to, why := tally.NextBackfill(db, mk, now)
	if m == nil || m.ID() != "coinbase:BTC-USD" || to.Format("2006-01-02") != "2026-09-23" || from.Format("2006-01-02") != "2025-11-28" || !strings.Contains(why, "back from 2026-09-24") {
		t.Fatalf("%v %s %s %s", m, from, to, why)
	}
	// coinbase done: the walk moves to the next venue that holds something
	tally.MarkHistoryDone(db, *m)
	if err := db.Exec("INSERT INTO crypto_daily (day, symbol, exchange, quote, open, high, low, close, volume) VALUES ('2010-01-01','BTC','kraken','USD',1,1,1,1,1)"); err != nil {
		t.Fatal(err)
	}
	// kraken's oldest is at the floor: marked done without a page; nothing else holds candles
	if m, _, _, why := tally.NextBackfill(db, mk, now); m != nil {
		t.Fatalf("%v %s", m, why)
	}
	rows, _ := db.Query("SELECT count(*) FROM settings WHERE key LIKE 'hist_done_%'")
	if *rows.Vals[0][0] != "2" {
		t.Fatalf("done marks: %v", rows.Vals)
	}
}
