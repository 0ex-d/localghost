package rates

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestTimePenalty(t *testing.T) {
	for _, c := range []struct {
		s    float64
		want float64
	}{{0, 1}, {60, 1}, {780, 0.5005}, {1500, 0.001}, {4000, 0.001}} {
		if got := TimePenalty(time.Duration(c.s * float64(time.Second))); math.Abs(got-c.want) > 0.001 {
			t.Fatalf("%v s: %v, want %v", c.s, got, c.want)
		}
	}
	if outlierBand(15) != 1.05 || outlierBand(12) != 1.10 || outlierBand(3) != 1.15 {
		t.Fatal("bands")
	}
}

func TestBlendWeighsVolumeAndFreshness(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	q := func(ex, quote string, p, v float64, ageS int) Quote {
		return Quote{Exchange: ex, Base: "SOL", QuoteCcy: quote, Price: p, Volume: v, At: now.Add(-time.Duration(ageS) * time.Second)}
	}
	conv := map[string]float64{"USD": 1, "USDT": 1.0, "BTC": 100_000, "EUR": 1.1}
	quotes := []Quote{
		q("coinbase", "USD", 200, 1000, 10),
		q("binance", "USDT", 202, 3000, 10),
		q("kraken", "BTC", 0.002, 1000, 10), // 200 dollars through BTC
		q("kraken", "EUR", 182, 1000, 1500), // 200.2 dollars, but 25 minutes old: a thousandth of the weight
		q("bitstamp", "USD", 260, 900, 10),  // 30% off the last price: out
		q("gemini", "GBP", 150, 9999, 10),   // no conversion for GBP: not a market here
	}
	ix, legs, err := Blend(quotes, "SOL", conv, 201, now)
	if err != nil {
		t.Fatal(err)
	}
	// (200·1000 + 202·3000 + 200·1000 + 200.2·1) / 5001
	want := (200.0*1000 + 202*3000 + 200*1000 + 200.2*1) / 5001
	if math.Abs(ix.Price-want) > 0.01 || ix.Markets != 4 || ix.N != 3 || strings.Join(ix.Paths, ",") != "USD,USDT,EUR,BTC" {
		t.Fatalf("%+v want %v", ix, want)
	}
	if legs[0].Exchange != "binance" || ix.Dropped["bitstamp:USD"] == "" || len(legs) != 5 {
		t.Fatalf("%+v", legs)
	}
	// no last price: the weighted median stands in, and the same market is still out
	if ix2, _, _ := Blend(quotes, "SOL", conv, 0, now); math.Abs(ix2.Price-want) > 0.01 {
		t.Fatal(ix2.Price)
	}
}

func TestBlendStableOnlyFromDollars(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	quotes := []Quote{
		{Exchange: "kraken", Base: "USDC", QuoteCcy: "USD", Price: 0.9999, Volume: 100, At: now},
		{Exchange: "binance", Base: "USDC", QuoteCcy: "USDT", Price: 1.0002, Volume: 100, At: now},
		{Exchange: "kraken", Base: "USDC", QuoteCcy: "EUR", Price: 0.5, Volume: 1e9, At: now}, // never: a stablecoin is priced in dollars
	}
	ix, _, err := Blend(quotes, "USDC", map[string]float64{"USD": 1, "USDT": 1, "EUR": 1.1}, 0, now)
	if err != nil || ix.Markets != 2 || math.Abs(ix.Price-1.00005) > 1e-6 {
		t.Fatalf("%+v %v", ix, err)
	}
}

func TestBlendAllConvertsInOrder(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	at := func(ex, base, quote string, p, v float64) Quote {
		return Quote{Exchange: ex, Base: base, QuoteCcy: quote, Price: p, Volume: v, At: now}
	}
	quotes := []Quote{
		at("kraken", "USDT", "USD", 1.001, 1e6),
		at("coinbase", "BTC", "USD", 100_000, 10),
		at("binance", "BTC", "USDT", 99_900, 10), // 99,999.9 dollars
		at("binance", "ETH", "BTC", 0.04, 100),   // through BTC
		at("kraken", "XYZ", "ETH", 0.001, 1000),  // through ETH, which came through BTC
	}
	ixs, _, failed := BlendAll(quotes, []string{"XYZ", "BTC", "ETH"}, 1.1, nil, now)
	if len(failed) != 0 || math.Abs(ixs["BTC"].Price-99_999.95) > 0.01 || math.Abs(ixs["ETH"].Price-3999.998) > 0.01 || math.Abs(ixs["XYZ"].Price-3.999998) > 1e-5 {
		t.Fatalf("%+v %v", ixs, failed)
	}
	if strings.Join(ixs["XYZ"].Paths, ",") != "ETH" {
		t.Fatal(ixs["XYZ"].Paths)
	}
}

func TestBatchKeepsTheConversionPairs(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	want := map[string]bool{"SOL": true}
	k, err := ParseBatch("kraken", []byte(`{"error":[],"result":{"SOLXBT":{"c":["0.002","1"],"v":["1","50"]},"XETHXXBT":{"c":["0.04","1"],"v":["1","9"]},"SOLEUR":{"c":["180","1"],"v":["1","40"]},"XXBTZEUR":{"c":["90000","1"],"v":["1","3"]},"SOLGBP":{"c":["150","1"],"v":["1","40"]}}}`), want, now)
	if err != nil || len(k) != 4 {
		t.Fatalf("%+v %v", k, err)
	}
	b, _ := ParseBatch("binance", []byte(`[{"symbol":"SOLBTC","lastPrice":"0.002","volume":"10"},{"symbol":"SOLUSDC","lastPrice":"200","volume":"10"},{"symbol":"SOLTRY","lastPrice":"9000","volume":"10"}]`), want, now)
	if len(b) != 2 || b[0].QuoteCcy != "BTC" || b[1].QuoteCcy != "USDC" {
		t.Fatalf("%+v", b)
	}
	f, _ := ParseBatch("bitfinex", []byte(`[["tSOLUST",0,0,0,0,0,0,201,10,0,0],["tBTCUDC",0,0,0,0,0,0,100000,1,0,0],["tSOLBTC",0,0,0,0,0,0,0.002,5,0,0]]`), want, now)
	if len(f) != 3 || f[0].QuoteCcy != "USDT" || f[1].Base != "BTC" || f[1].QuoteCcy != "USDC" {
		t.Fatalf("%+v", f)
	}
	c, _ := ParseBatch("coinbase", []byte(`{"products":[{"product_id":"SOL-USD","price":"200","volume_24h":"5000","base_currency_id":"SOL","quote_currency_id":"USD","status":"online","trading_disabled":false},
		{"product_id":"SOL-EUR","price":"181","volume_24h":"100","base_currency_id":"SOL","quote_currency_id":"EUR","status":"online","trading_disabled":true},
		{"product_id":"ETH-USDC","price":"4000","volume_24h":"10","base_currency_id":"ETH","quote_currency_id":"USDC","status":"online","trading_disabled":false},
		{"product_id":"DOGE-USD","price":"0.2","volume_24h":"10","base_currency_id":"DOGE","quote_currency_id":"USD","status":"online","trading_disabled":false}]}`), want, now)
	if len(c) != 2 || c[0].Base != "SOL" || c[1].Base != "ETH" {
		t.Fatalf("%+v", c)
	}
}

func TestParseCoinInfo(t *testing.T) {
	info := ParseCoinInfo([]byte(`{"data":[{"symbol":"btc","name":"Bitcoin","description":" The first. ","color":"#F7931A","listed":true,
		"resource_urls":[{"type":"white_paper","link":"https://bitcoin.org/bitcoin.pdf"},{"type":"website","link":"https://bitcoin.org"},{"type":"website","link":"javascript:alert(1)"}]},
		{"symbol":"OLD","name":"Gone","listed":false}]}`))
	if len(info) != 1 || info[0].Symbol != "BTC" || info[0].Description != "The first." || info[0].Color != "#F7931A" ||
		info[0].Website != "https://bitcoin.org" || info[0].WhitePaper != "https://bitcoin.org/bitcoin.pdf" {
		t.Fatalf("%+v", info)
	}
}
