package rates

import (
	"strings"
	"testing"
	"time"
)

func TestFastAsks(t *testing.T) {
	asks := FastAsks([]string{"BTC", "eth", "SOL", "USDT"})
	venues := map[string]int{}
	for _, a := range asks {
		venues[a.Venue]++
	}
	// one call each for Binance, Bitfinex and Kraken; one per coin for Bitstamp, Coinbase and OKX;
	// Gemini (no volume) never
	if len(asks) != 12 || venues["binance"] != 1 || venues["kraken"] != 1 || venues["bitfinex"] != 1 ||
		venues["coinbase"] != 3 || venues["bitstamp"] != 3 || venues["okx"] != 3 || venues["gemini"] != 0 {
		t.Fatalf("%d %v", len(asks), venues)
	}
	for _, a := range asks {
		switch a.Venue {
		case "binance":
			if !strings.Contains(a.URL, "symbols=%5B%22BTCUSDT%22%2C%22ETHUSDT%22%2C%22SOLUSDT%22%5D") {
				t.Fatal(a.URL)
			}
		case "kraken":
			if !strings.HasSuffix(a.URL, "pair=XBTUSD,ETHUSD,SOLUSD") {
				t.Fatal(a.URL)
			}
		case "bitfinex":
			if !strings.HasSuffix(a.URL, "symbols=tBTCUSD,tETHUSD,tSOLUSD") {
				t.Fatal(a.URL)
			}
		}
	}
	if FastAsks([]string{"USDT"}) != nil {
		t.Fatal("no coin, no ask")
	}
}

func TestFastIndexAcrossVenues(t *testing.T) {
	now := time.Unix(1_790_000_000, 0)
	want := map[string]bool{"BTC": true, "ETH": true}
	var quotes []Quote
	for _, a := range FastAsks([]string{"BTC", "ETH"}) {
		var body string
		switch {
		case a.Venue == "binance":
			body = `[{"symbol":"BTCUSDT","lastPrice":"100100","volume":"10","closeTime":1790000000000},{"symbol":"ETHUSDT","lastPrice":"4000","volume":"100","closeTime":1790000000000}]`
		case a.Venue == "kraken":
			body = `{"error":[],"result":{"XXBTZUSD":{"c":["100000.0","1"],"v":["1","10"]},"XETHZUSD":{"c":["4001","1"],"v":["1","100"]}}}`
		case a.Venue == "bitfinex":
			body = `[["tBTCUSD",1,1,1,1,1,1,99900,10,1,1],["tETHUSD",1,1,1,1,1,1,3999,100,1,1]]`
		case a.Market.Base == "BTC" && a.Venue == "coinbase":
			body = `{"price":"100050","volume":"10","time":"2026-09-21T17:46:40Z"}`
		default:
			continue // the others did not answer this time
		}
		qs, err := ParseFast(a, []byte(body), want, now)
		if err != nil {
			t.Fatalf("%s: %v", a.ID, err)
		}
		quotes = append(quotes, qs...)
	}
	ix, err := FastIndex(quotes, "BTC", 0.999, now)
	if err != nil || ix.N != 4 || ix.Price < 99_900 || ix.Price > 100_100 {
		t.Fatalf("%+v %v", ix, err)
	}
	if strings.Join(ix.Used, ",") != "binance,bitfinex,coinbase,kraken" {
		t.Fatal(ix.Used)
	}
	if eth, err := FastIndex(quotes, "ETH", 0.999, now); err != nil || eth.N != 3 {
		t.Fatalf("%+v %v", eth, err)
	}
}
