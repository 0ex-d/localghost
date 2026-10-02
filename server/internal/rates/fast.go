package rates

// THE FAST LANE's asks: BTC, ETH and SOL from every venue that weighs in the index, every five
// seconds. One call where the venue takes several pairs at once (Binance, Kraken, Bitfinex), one
// per pair where it does not (Coinbase, Bitstamp, OKX's single ticker; its all-pairs answer is
// hundreds of instruments). Gemini is not asked: its feed gives no volume, so the index leaves it
// out anyway. The USDT/USD leg is not asked either: it moves in the fourth decimal, and the
// minute's rate folds Binance's and OKX's USDT prices into dollars. Each coin is blended the
// minute's way (Blend).

import (
	"net/url"
	"strings"
	"time"
)

// FastAsk is one call of the fast lane. Market is set for a one-pair ticker; zero for a venue's
// several-pairs answer (read like its all-pairs one, ParseBatch).
type FastAsk struct {
	Venue  string
	ID     string
	URL    string
	Market Market
}

// FastVenues are the venues the fast lane asks, in the index's order.
var FastVenues = []string{"binance", "bitfinex", "bitstamp", "coinbase", "kraken", "okx"}

// FastAsks is every call for the symbols.
func FastAsks(symbols []string) []FastAsk {
	var syms []string
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s != "" && s != "USD" && s != "USDT" {
			syms = append(syms, s)
		}
	}
	if len(syms) == 0 {
		return nil
	}
	var out []FastAsk
	pairs := func(ex, quote string) []string {
		var p []string
		for _, s := range syms {
			p = append(p, exchangeSymbol(Market{ex, s, quote}))
		}
		return p
	}
	// Binance: ["BTCUSDT","ETHUSDT"], the mini answer (price, volume, close time)
	b := pairs("binance", "USDT")
	out = append(out, FastAsk{Venue: "binance", ID: "fast:binance",
		URL: "https://api.binance.com/api/v3/ticker/24hr?type=MINI&symbols=" + url.QueryEscape(`["`+strings.Join(b, `","`)+`"]`)})
	out = append(out, FastAsk{Venue: "bitfinex", ID: "fast:bitfinex",
		URL: "https://api-pub.bitfinex.com/v2/tickers?symbols=" + strings.Join(pairs("bitfinex", "USD"), ",")})
	out = append(out, FastAsk{Venue: "kraken", ID: "fast:kraken",
		URL: "https://api.kraken.com/0/public/Ticker?pair=" + strings.Join(pairs("kraken", "USD"), ",")})
	for _, s := range syms {
		for _, m := range []Market{{"bitstamp", s, "USD"}, {"coinbase", s, "USD"}, {"okx", s, "USDT"}} {
			out = append(out, FastAsk{Venue: m.Exchange, ID: "fast:" + m.ID(), URL: m.TickerURL(), Market: m})
		}
	}
	return out
}

// ParseFast reads one fast answer into quotes.
func ParseFast(a FastAsk, body []byte, want map[string]bool, fetched time.Time) ([]Quote, error) {
	if a.Market.Exchange == "" {
		return ParseBatch(a.Venue, body, want, fetched)
	}
	q, err := ParseTicker(a.Market, body, fetched)
	if err != nil {
		return nil, err
	}
	if q.Price <= 0 {
		return nil, errNoPairs(a.Venue)
	}
	return []Quote{q}, nil
}

// FastIndex is a symbol's price from the fast lane's quotes, blended the minute's way (Blend),
// USDT prices converted at usdt, outliers against prev (the coin's last price, 0 for none).
func FastIndex(quotes []Quote, symbol string, usdt, prev float64, now time.Time) (Index, error) {
	if usdt <= 0 {
		usdt = 1
	}
	ix, _, err := Blend(quotes, symbol, map[string]float64{"USD": 1, "USDT": usdt}, prev, now)
	return ix, err
}
