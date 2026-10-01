package rates

import (
	"testing"
	"time"
)

func TestParseBatchSixVenues(t *testing.T) {
	fetched := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	want := map[string]bool{"BTC": true, "SOL": true, "DOGE": true}
	cases := []struct {
		ex   string
		body string
		n    int // quotes kept
	}{
		{"binance", `[{"symbol":"BTCUSDT","lastPrice":"65000","volume":"1000","closeTime":1790848800000},{"symbol":"SOLUSDT","lastPrice":"150","volume":"5000","closeTime":1790848800000},{"symbol":"BTCEUR","lastPrice":"60000","volume":"1"},{"symbol":"ADAUSDT","lastPrice":"0.4","volume":"1"},{"symbol":"USDTTRY","lastPrice":"40","volume":"1"}]`, 2},
		{"okx", `{"code":"0","data":[{"instId":"BTC-USDT","last":"65010","vol24h":"900","ts":"1790848800000"},{"instId":"DOGE-USDT","last":"0.12","vol24h":"1e6","ts":"1790848800000"},{"instId":"BTC-USDC","last":"1","vol24h":"1","ts":"1"},{"instId":"ETH-USDT","last":"3500","vol24h":"1","ts":"1"}]}`, 2},
		{"kraken", `{"error":[],"result":{"XXBTZUSD":{"c":["65020","1"],"v":["10","800"]},"SOLUSD":{"c":["151","1"],"v":["1","4000"]},"XXDGZUSD":{"c":["0.121","1"],"v":["1","2e6"]},"USDTZUSD":{"c":["0.9991","1"],"v":["1","1e6"]},"XETHZUSD":{"c":["3500","1"],"v":["1","1"]},"XXBTZEUR":{"c":["60000","1"],"v":["1","1"]}}}`, 4},
		{"bitfinex", `[["tBTCUSD",64990,1,65010,1,120,0.0018,65005,3200,65500,64000],["tSOLUSD",149,1,151,1,1,0.01,150.5,20000,155,145],["tUSTUSD",0.999,1,1.0,1,0,0,0.9992,5e6,1,0.99],["tETHUSD",1,1,1,1,1,1,3500,1,1,1],["fUSD",0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]]`, 3},
		{"bitstamp", `[{"pair":"BTC/USD","last":"64950","volume":"2000","timestamp":"1790848800"},{"pair":"SOL/USD","last":"150.2","volume":"3000","timestamp":"1790848800"},{"pair":"USDT/USD","last":"0.9990","volume":"1e6","timestamp":"1790848800"},{"pair":"BTC/EUR","last":"60000","volume":"1","timestamp":"1"}]`, 3},
		{"gemini", `[{"pair":"BTCUSD","price":"65050","percentChange24h":"0.01"},{"pair":"SOLUSD","price":"150.1","percentChange24h":"0"},{"pair":"BTCGBP","price":"50000","percentChange24h":"0"},{"pair":"ETHUSD","price":"3500","percentChange24h":"0"}]`, 2},
	}
	for _, c := range cases {
		qs, err := ParseBatch(c.ex, []byte(c.body), want, fetched)
		if err != nil || len(qs) != c.n {
			t.Fatalf("%s: %d quotes %v: %+v", c.ex, len(qs), err, qs)
		}
		for _, q := range qs {
			if q.Exchange != c.ex || q.Price <= 0 || (q.QuoteCcy != "USD" && q.QuoteCcy != "USDT") || (!want[q.Base] && q.Base != "USDT") {
				t.Fatalf("%s: %+v", c.ex, q)
			}
		}
	}
	// the stamped venues keep their own time
	qs, _ := ParseBatch("binance", []byte(`[{"symbol":"BTCUSDT","lastPrice":"65000","volume":"1000","closeTime":1790848800000}]`), want, fetched)
	if qs[0].At.Unix() != 1790848800 {
		t.Fatalf("binance time: %v", qs[0].At)
	}
	if _, err := ParseBatch("binance", []byte(`{"code":-1003,"msg":"Too many requests"}`), want, fetched); err == nil {
		t.Fatal("an error body passed")
	}
	if _, err := ParseBatch("nowhere", []byte(`[]`), want, fetched); err == nil {
		t.Fatal("unknown venue passed")
	}
}

func TestKrakenAndBitfinexPairs(t *testing.T) {
	for key, want := range map[string][2]string{
		"XXBTZUSD": {"BTC", "USD"}, "XETHZUSD": {"ETH", "USD"}, "SOLUSD": {"SOL", "USD"}, "USDTZUSD": {"USDT", "USD"},
		"XXDGZUSD": {"DOGE", "USD"}, "SOLUSDT": {"SOL", "USDT"}, "XXBTZEUR": {"", ""}, "": {"", ""},
	} {
		b, q := KrakenPair(key)
		if b != want[0] || q != want[1] {
			t.Fatalf("%s: %s/%s", key, b, q)
		}
	}
	for sym, want := range map[string][2]string{
		"tBTCUSD": {"BTC", "USD"}, "tSOLUST": {"SOL", "USDT"}, "tUSTUSD": {"USDT", "USD"}, "tMATIC:USD": {"MATIC", "USD"}, "tBTCEUR": {"", ""}, "fUSD": {"", ""},
	} {
		b, q := bitfinexPair(sym)
		if b != want[0] || q != want[1] {
			t.Fatalf("%s: %s/%s", sym, b, q)
		}
	}
}

func TestSymbolsFromRanks(t *testing.T) {
	coins := []Coin{{1, "bitcoin", "BTC", "Bitcoin", 1, 1, 1, 0}, {2, "ethereum", "ETH", "Ethereum", 1, 1, 1, 0}, {3, "tether", "USDT", "Tether", 1, 1, 1, 0},
		{4, "x", "XRP", "XRP", 1, 1, 1, 0}, {5, "usdc", "USDC", "USDC", 1, 1, 1, 0}, {6, "wbtc", "WBTC", "Wrapped", 1, 1, 1, 0}, {7, "sol", "SOL", "Solana", 1, 1, 1, 0}}
	got := SymbolsFromRanks(coins, 4)
	if len(got) != 4 || got[0] != "BTC" || got[1] != "ETH" || got[2] != "XRP" || got[3] != "SOL" {
		t.Fatalf("%v", got)
	}
}
