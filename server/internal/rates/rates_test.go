package rates

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

const ecb = `<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
<gesmes:subject>Reference rates</gesmes:subject>
<Cube><Cube time="2026-09-30">
<Cube currency="USD" rate="1.1734"/><Cube currency="JPY" rate="173.21"/><Cube currency="BGN" rate="1.9558"/>
<Cube currency="CZK" rate="24.30"/><Cube currency="DKK" rate="7.46"/><Cube currency="GBP" rate="0.8712"/>
<Cube currency="HUF" rate="390.1"/><Cube currency="PLN" rate="4.27"/><Cube currency="RON" rate="5.08"/>
<Cube currency="SEK" rate="11.02"/><Cube currency="CHF" rate="0.935"/>
</Cube><Cube time="2026-09-29">
<Cube currency="USD" rate="1.17"/><Cube currency="JPY" rate="173"/><Cube currency="BGN" rate="1.9558"/>
<Cube currency="CZK" rate="24.3"/><Cube currency="DKK" rate="7.46"/><Cube currency="GBP" rate="0.87"/>
<Cube currency="HUF" rate="390"/><Cube currency="PLN" rate="4.27"/><Cube currency="RON" rate="5.07"/>
<Cube currency="SEK" rate="11"/><Cube currency="CHF" rate="0.93"/>
</Cube></Cube></gesmes:Envelope>`

func TestParseECB(t *testing.T) {
	day, fx, err := ParseECB([]byte(ecb))
	if err != nil {
		t.Fatal(err)
	}
	if day != "2026-09-30" || fx["GBP"] != 0.8712 || fx["RON"] != 5.08 || len(fx) != 11 {
		t.Fatalf("%s %v", day, fx)
	}
	all, err := ParseECBAll([]byte(ecb))
	if err != nil || len(all) != 2 || all[1].Day != "2026-09-29" || all[1].Rates["GBP"] != 0.87 {
		t.Fatalf("all days: %v %v", all, err)
	}
	if _, _, err := ParseECB([]byte("<html>no</html>")); err == nil {
		t.Fatal("html passed")
	}
}

func TestMarketsAndURLs(t *testing.T) {
	ms := Markets([]string{"BTC", "eth", "usdt", ""})
	ids := map[string]bool{}
	for _, m := range ms {
		ids[m.ID()] = true
	}
	for _, want := range []string{"coinbase:BTC-USD", "kraken:ETH-USD", "binance:BTC-USDT", "okx:ETH-USDT", "bitfinex:BTC-USD", "coinbase:USDT-USD", "kraken:USDT-USD"} {
		if !ids[want] {
			t.Fatalf("missing %s in %v", want, ids)
		}
	}
	if ids["binance:USDT-USD"] || len(ms) != 2*7+4 {
		t.Fatalf("markets: %d %v", len(ms), ids)
	}
	m, ok := ParseMarketID("kraken:BTC-USD")
	if !ok || m.Exchange != "kraken" || m.Base != "BTC" || m.Quote != "USD" || m.TickerURL() != "https://api.kraken.com/0/public/Ticker?pair=XBTUSD" {
		t.Fatalf("%+v %s", m, m.TickerURL())
	}
	if _, ok := ParseMarketID("nonsense"); ok {
		t.Fatal("bad id parsed")
	}
	if u := (Market{"bitfinex", "USDT", "USD"}).TickerURL(); u != "https://api-pub.bitfinex.com/v2/ticker/tUSTUSD" {
		t.Fatal(u)
	}
	if u := (Market{"binance", "BTC", "USDT"}).TickerURL(); !strings.HasSuffix(u, "symbol=BTCUSDT") {
		t.Fatal(u)
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if u := (Market{"coinbase", "BTC", "USD"}).CandlesURL(from, from.AddDate(0, 0, 10)); !strings.Contains(u, "granularity=86400&start=2026-01-01T00:00:00Z") {
		t.Fatal(u)
	}
	// twelve symbols: the phone's three (the ECB twice and Coinbase's rank list), then six batch
	// venues once each and Coinbase for the ten largest and USDT, every minute
	syms := []string{"BTC", "ETH", "SOL", "XRP", "BNB", "DOGE", "ADA", "TRX", "AVAX", "LINK", "TON", "DOT"}
	ph := PhoneSources()
	if len(ph) != 3 || ph[0].ID != "ecb" || ph[2].ID != CoinbaseRanks || !strings.HasPrefix(ph[2].URL, "https://www.coinbase.com/") {
		t.Fatalf("phone sources: %v", ph)
	}
	tk := TickerSources(syms)
	if tk[0].ID != "binance:all" || tk[5].ID != "gemini:all" || tk[6].ID != "coinbase:BTC-USD" || tk[len(tk)-1].ID != "coinbase:USDT-USD" {
		t.Fatalf("tickers: %v", tk)
	}
	cb := 0
	for _, s := range tk {
		if strings.HasPrefix(s.ID, "coinbase:") {
			cb++
		}
		if s.Every != 1 {
			t.Fatalf("a ticker every %d min", s.Every)
		}
	}
	if cb != CoinbaseTop+1 || len(tk) != 6+CoinbaseTop+1 || len(Sources(syms, from)) != 3+len(tk) {
		t.Fatalf("coinbase %d, tickers %d", cb, len(tk))
	}
	if u := (Market{"binance", "BTC", "USDT"}).HourlyURL(from, from.Add(time.Hour)); !strings.Contains(u, "interval=1h") {
		t.Fatal(u)
	}
	if u := (Market{"kraken", "BTC", "USD"}).HourlyURL(from, from); !strings.Contains(u, "interval=60") {
		t.Fatal(u)
	}
	if u := (Market{"okx", "BTC", "USDT"}).HourlyURL(from, from); !strings.Contains(u, "bar=1H") {
		t.Fatal(u)
	}
}

func TestParseTickers(t *testing.T) {
	fetched := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		m          Market
		body       string
		price, vol float64
		atSec      int64
	}{
		{Market{"coinbase", "BTC", "USD"}, `{"ask":"65001","bid":"64999","volume":"12345.678","price":"65000.01","time":"2026-10-01T09:59:30.123456Z"}`, 65000.01, 12345.678, fetched.Unix() - 30},
		{Market{"kraken", "BTC", "USD"}, `{"error":[],"result":{"XXBTZUSD":{"a":["65100.1","1","1.000"],"c":["65100.0","0.01"],"v":["1234.5","2345.6"]}}}`, 65100, 2345.6, fetched.Unix()},
		{Market{"bitstamp", "BTC", "USD"}, `{"timestamp":"1759312770","last":"64950","volume":"2000.5"}`, 64950, 2000.5, 1759312770},
		{Market{"gemini", "BTC", "USD"}, `{"bid":"1","ask":"2","volume":{"BTC":"800.25","USD":"52000000","timestamp":1759312800000},"last":"65050"}`, 65050, 800.25, 1759312800},
		{Market{"binance", "BTC", "USDT"}, `{"symbol":"BTCUSDT","lastPrice":"65080.50","volume":"18000.1","quoteVolume":"1.1e9","closeTime":1759312800123}`, 65080.5, 18000.1, 1759312800},
		{Market{"bitfinex", "BTC", "USD"}, `[64990,1.2,65010,0.8,120,0.0018,65005,3200.5,65500,64000]`, 65005, 3200.5, fetched.Unix()},
		{Market{"okx", "ETH", "USDT"}, `{"code":"0","data":[{"instId":"ETH-USDT","last":"3500.5","vol24h":"90000","ts":"1759312800000"}]}`, 3500.5, 90000, 1759312800},
	}
	for _, c := range cases {
		q, err := ParseTicker(c.m, []byte(c.body), fetched)
		if err != nil || q.Price != c.price || q.Volume != c.vol || q.At.Unix() != c.atSec || q.Base != c.m.Base || q.QuoteCcy != c.m.Quote {
			t.Fatalf("%s: %+v %v", c.m.ID(), q, err)
		}
	}
	if _, err := ParseTicker(Market{"coinbase", "BTC", "USD"}, []byte(`{"message":"rate limited"}`), fetched); err == nil {
		t.Fatal("no price passed")
	}
	if _, err := ParseTicker(Market{"bitfinex", "BTC", "USD"}, []byte(`{"error":"x"}`), fetched); err == nil {
		t.Fatal("bitfinex error body passed")
	}
}

func TestParseCandles(t *testing.T) {
	cases := []struct {
		m    Market
		body string
	}{
		{Market{"coinbase", "BTC", "USD"}, `[[1790812800,64000,66000,64500,65500,1200.5],[1790726400,63000,65000,63500,64500,1000]]`},
		{Market{"kraken", "BTC", "USD"}, `{"result":{"XXBTZUSD":[[1790726400,"63500","65000","63000","64500","64100","1000","20"],[1790812800,"64500","66000","64000","65500","65000","1200.5","30"]],"last":1790812800}}`},
		{Market{"bitstamp", "BTC", "USD"}, `{"data":{"pair":"BTC/USD","ohlc":[{"timestamp":"1790726400","open":"63500","high":"65000","low":"63000","close":"64500","volume":"1000"},{"timestamp":"1790812800","open":"64500","high":"66000","low":"64000","close":"65500","volume":"1200.5"}]}}`},
		{Market{"gemini", "BTC", "USD"}, `[[1790812800000,64500,66000,64000,65500,1200.5],[1790726400000,63500,65000,63000,64500,1000]]`},
		{Market{"binance", "BTC", "USDT"}, `[[1790726400000,"63500","65000","63000","64500","1000",1790812799999,"6e7",100,"500","3e7","0"],[1790812800000,"64500","66000","64000","65500","1200.5",1790899199999,"7e7",120,"600","4e7","0"]]`},
		{Market{"bitfinex", "BTC", "USD"}, `[[1790726400000,63500,64500,65000,63000,1000],[1790812800000,64500,65500,66000,64000,1200.5]]`},
		{Market{"okx", "BTC", "USDT"}, `{"code":"0","data":[["1790812800000","64500","66000","64000","65500","1200.5","7e7","7e7","1"],["1790726400000","63500","65000","63000","64500","1000","6e7","6e7","1"]]}`},
	}
	for _, c := range cases {
		cs, err := ParseCandles(c.m, []byte(c.body))
		if err != nil || len(cs) != 2 {
			t.Fatalf("%s: %v %v", c.m.ID(), cs, err)
		}
		// newest first, the same two days from every venue, the same numbers
		if cs[0].Day != "2026-10-01" || cs[1].Day != "2026-09-30" || cs[0].Open != 64500 || cs[0].High != 66000 || cs[0].Low != 64000 || cs[0].Close != 65500 || cs[0].Volume != 1200.5 {
			t.Fatalf("%s: %+v", c.m.ID(), cs)
		}
	}
	if _, err := ParseCandles(Market{"coinbase", "BTC", "USD"}, []byte(`{"message":"NotFound"}`)); err == nil {
		t.Fatal("an error body passed")
	}
}

func TestIndexMethodAndUSDT(t *testing.T) {
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	qs := []Quote{
		{"coinbase", "BTC", "USD", 65000, 10000, now.Add(-time.Minute)},
		{"kraken", "BTC", "USD", 65100, 2000, now},
		{"bitstamp", "BTC", "USD", 64950, 2000, now.Add(-2 * time.Minute)},
		{"gemini", "BTC", "USD", 70000, 800, now},                 // 7.7% off the median: out
		{"stale", "BTC", "USD", 65020, 5000, now.Add(-time.Hour)}, // an hour old: out
		{"binance", "BTC", "USDT", 65065, 4000, now},              // in USDT: folded at 0.999
		{"coinbase", "USDT", "USD", 0.9990, 1e6, now},
		{"kraken", "USDT", "USD", 0.9992, 1e6, now},
		{"bitstamp", "USDT", "USD", 0.9988, 1e6, now.Add(-time.Hour)}, // stale: not in the USDT rate
	}
	usdt, ok := USDTRate(qs, now)
	if !ok || math.Abs(usdt-0.9991) > 1e-9 {
		t.Fatalf("usdt: %v %v", usdt, ok)
	}
	btc := InUSD(qs, "BTC", usdt)
	if len(btc) != 6 {
		t.Fatalf("btc quotes: %d", len(btc))
	}
	ix, err := MakeIndex(btc, now)
	if err != nil {
		t.Fatal(err)
	}
	binanceUSD := 65065 * usdt
	want := (65000*10000 + 65100*2000 + 64950*2000 + binanceUSD*4000) / 18000.0
	if ix.N != 4 || math.Abs(ix.Price-want) > 0.01 {
		t.Fatalf("index: %+v (want %.2f)", ix, want)
	}
	if ix.Dropped["gemini"] == "" || ix.Dropped["stale"] == "" || len(ix.Used) != 4 || ix.Used[0] != "binance" {
		t.Fatalf("dropped/used: %+v", ix)
	}
	if r, ok := USDTRate(nil, now); ok || r != 1 {
		t.Fatal("no usdt legs: 1 and false")
	}
	// no volumes anywhere: a plain mean
	plain, _ := MakeIndex([]Quote{{"a", "X", "USD", 100, 0, now}, {"b", "X", "USD", 102, 0, now}}, now)
	if plain.Price != 101 || plain.N != 2 {
		t.Fatalf("plain: %+v", plain)
	}
	if _, err := MakeIndex([]Quote{{"a", "X", "USD", 100, 1, now.Add(-time.Hour)}}, now); err == nil {
		t.Fatal("stale alone passed")
	}
}

func TestDailyIndex(t *testing.T) {
	closes := map[Market]float64{
		{"coinbase", "BTC", "USD"}: 65000, {"kraken", "BTC", "USD"}: 65100, {"binance", "BTC", "USDT"}: 65100, {"okx", "BTC", "USDT"}: 65200,
		{"gemini", "BTC", "USD"}:   80000, // a bad print, out
		{"coinbase", "ETH", "USD"}: 3500,
	}
	p, n := DailyIndex(closes, "BTC", 0.999)
	want := (65000 + 65100 + 65100*0.999 + 65200*0.999) / 4
	if n != 4 || math.Abs(p-want) > 1e-6 {
		t.Fatalf("%v %d (want %.4f)", p, n, want)
	}
	if p, n := DailyIndex(closes, "ETH", 0); n != 1 || p != 3500 {
		t.Fatalf("eth: %v %d", p, n)
	}
	if _, n := DailyIndex(closes, "SOL", 1); n != 0 {
		t.Fatal("sol")
	}
}

// Coinbase's list as coinbase.com's price pages read it: numbers as strings, the day's change as a
// fraction of one, the circulating supply, a coin it no longer lists left out.
func TestParseCoinbaseRanks(t *testing.T) {
	var rows []string
	for i := 1; i <= 12; i++ {
		rows = append(rows, fmt.Sprintf(`{"id":"u%d","symbol":"c%d","name":"Coin %d","slug":"coin-%d","listed":%v,"rank":%d,"market_cap":"%d000.5","latest":"%d.25","volume_24h":"77.5","percent_change":-0.0123,"circulating_supply":"%d500"}`,
			i, i, i, i, i != 3, i, 100-i, i, i))
	}
	body := `{"pagination":{"limit":100},"data":[` + strings.Join(rows, ",") + `]}`
	coins, err := ParseCoins(CoinbaseRanks, []byte(body))
	if err != nil || len(coins) != 11 {
		t.Fatalf("coinbase: %d %v", len(coins), err)
	}
	c := coins[0]
	if c.Symbol != "C1" || c.ID != "coin-1" || c.Rank != 1 || c.PriceUSD != 1.25 || c.MarketCap != 99000.5 || c.Volume24 != 77.5 || math.Abs(c.Change24+1.23) > 1e-9 || c.Supply != 1500 {
		t.Fatalf("first: %+v", c)
	}
	if coins[2].Symbol != "C4" {
		t.Fatalf("the unlisted coin stayed: %+v", coins[2])
	}
	if _, err := ParseCoins(CoinbaseRanks, []byte(`{"errors":[{"id":"not_found"}]}`)); err == nil {
		t.Fatal("an error body passed")
	}
	if _, err := ParseCoins("coingecko", []byte(`[]`)); err == nil {
		t.Fatal("a list the box no longer reads was read")
	}
	if !IsRankSource(CoinbaseRanks) || IsRankSource("coingecko") || IsRankSource("coinbase:BTC-USD") {
		t.Fatal("rank sources")
	}
}

func repeat(tpl string, n int) string {
	out := ""
	for i := 1; i <= n; i++ {
		if i > 1 {
			out += ","
		}
		out += strings.ReplaceAll(tpl, "%d", strconv.Itoa(i))
	}
	return out
}

func TestConvert(t *testing.T) {
	fx := map[string]float64{"USD": 1.2, "GBP": 0.9, "RON": 5.0}
	usd := map[string]float64{"BTC": 60000, "ETH": 3000, "USDT": 1.0}
	if v, err := Convert(100, "EUR", "GBP", fx, usd); err != nil || math.Abs(v-90) > 1e-9 {
		t.Fatalf("eur→gbp %v %v", v, err)
	}
	if v, err := Convert(100, "gbp", "ron", fx, usd); err != nil || math.Abs(v-100/0.9*5) > 1e-9 {
		t.Fatalf("gbp→ron %v %v", v, err)
	}
	if v, err := Convert(2, "BTC", "USD", fx, usd); err != nil || math.Abs(v-120000) > 1e-6 {
		t.Fatalf("btc→usd %v %v", v, err)
	}
	if v, err := Convert(60000, "USD", "XBT", fx, usd); err != nil || math.Abs(v-1) > 1e-9 {
		t.Fatalf("usd→btc %v %v", v, err)
	}
	if v, err := Convert(1, "BTC", "ETH", fx, usd); err != nil || math.Abs(v-20) > 1e-9 {
		t.Fatalf("btc→eth %v %v", v, err)
	}
	if _, err := Convert(1, "EUR", "XYZ", fx, usd); err == nil {
		t.Fatal("unknown code passed")
	}
	if _, err := Convert(1, "BTC", "EUR", fx, nil); err == nil {
		t.Fatal("no btc price passed")
	}
}
