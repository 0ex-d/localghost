package rates

// FIFTY COINS IN A FEW CALLS. Asking a venue for one pair at a time was fine for two symbols; for
// the top fifty it is three hundred and fifty calls an hour. Six of the seven venues answer every
// pair they trade in one call; the box (or the phone) fetches that once and keeps the pairs it
// follows. Coinbase has no such call, so it is asked pair by pair for the ten largest (and the
// USDT leg). A pair a venue does not trade is simply absent from its answer; nothing fails.

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// BatchExchanges are the venues asked for everything at once.
var BatchExchanges = []string{"binance", "okx", "kraken", "bitfinex", "bitstamp", "gemini"}

// BatchURL is the venue's all-pairs ticker call.
func BatchURL(exchange string) string {
	switch exchange {
	case "binance": // MINI: the price, the volume and the close time, a third of the full answer
		return "https://api.binance.com/api/v3/ticker/24hr?type=MINI"
	case "okx":
		return "https://www.okx.com/api/v5/market/tickers?instType=SPOT"
	case "kraken":
		return "https://api.kraken.com/0/public/Ticker"
	case "bitfinex":
		return "https://api-pub.bitfinex.com/v2/tickers?symbols=ALL"
	case "bitstamp":
		return "https://www.bitstamp.net/api/v2/ticker/"
	case "gemini":
		return "https://api.gemini.com/v1/pricefeed"
	}
	return ""
}

// CoinbaseTop is how many of the largest symbols Coinbase is asked for one by one.
const CoinbaseTop = 10

// ParseBatch reads a venue's all-pairs answer and keeps the pairs for the symbols wanted (USD and
// USDT quotes), plus the USDT/USD leg. fetched is when it was fetched; a venue that stamps its
// quotes keeps its own time.
func ParseBatch(exchange string, body []byte, want map[string]bool, fetched time.Time) ([]Quote, error) {
	num := func(v any) float64 {
		switch x := v.(type) {
		case float64:
			return x
		case string:
			f, _ := strconv.ParseFloat(x, 64)
			return f
		}
		return 0
	}
	keep := func(base, quote string) bool {
		if base == "" || (quote != "USD" && quote != "USDT") {
			return false
		}
		if base == "USDT" {
			return quote == "USD"
		}
		return want[base]
	}
	var out []Quote
	add := func(base, quote string, price, vol float64, at time.Time) {
		if price > 0 && keep(base, quote) {
			out = append(out, Quote{Exchange: exchange, Base: base, QuoteCcy: quote, Price: price, Volume: vol, At: at})
		}
	}
	switch exchange {
	case "binance": // [{symbol:"BTCUSDT", lastPrice, volume, closeTime}]
		var rows []map[string]any
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			sym, _ := r["symbol"].(string)
			base, quote := splitSuffix(sym, "USDT")
			if base == "" {
				continue
			}
			at := fetched
			if ts := num(r["closeTime"]); ts > 0 {
				at = time.UnixMilli(int64(ts))
			}
			add(base, quote, num(r["lastPrice"]), num(r["volume"]), at)
		}
	case "okx": // {data:[{instId:"BTC-USDT", last, vol24h, ts}]}
		var obj struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(body, &obj); err != nil {
			return nil, err
		}
		for _, r := range obj.Data {
			inst, _ := r["instId"].(string)
			base, quote, ok := strings.Cut(inst, "-")
			if !ok {
				continue
			}
			at := fetched
			if ts := num(r["ts"]); ts > 0 {
				at = time.UnixMilli(int64(ts))
			}
			add(base, quote, num(r["last"]), num(r["vol24h"]), at)
		}
	case "kraken": // {result:{"XXBTZUSD":{c:[last,...], v:[today, 24h]}}}
		var obj struct {
			Result map[string]struct {
				C []any `json:"c"`
				V []any `json:"v"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &obj); err != nil {
			return nil, err
		}
		for key, t := range obj.Result {
			base, quote := KrakenPair(key)
			if base == "" || len(t.C) == 0 {
				continue
			}
			vol := 0.0
			if len(t.V) > 1 {
				vol = num(t.V[1])
			}
			add(base, quote, num(t.C[0]), vol, fetched)
		}
	case "bitfinex": // [[SYMBOL, BID, BID_SIZE, ASK, ASK_SIZE, CHG, CHG_REL, LAST, VOLUME, HIGH, LOW], ...]
		var rows [][]any
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if len(r) < 9 {
				continue
			}
			sym, _ := r[0].(string)
			base, quote := bitfinexPair(sym)
			add(base, quote, num(r[7]), num(r[8]), fetched)
		}
	case "bitstamp": // [{pair:"BTC/USD", last, volume, timestamp}]
		var rows []map[string]any
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			pair, _ := r["pair"].(string)
			base, quote, ok := strings.Cut(pair, "/")
			if !ok {
				continue
			}
			at := fetched
			if ts := num(r["timestamp"]); ts > 0 {
				at = time.Unix(int64(ts), 0)
			}
			add(strings.ToUpper(base), strings.ToUpper(quote), num(r["last"]), num(r["volume"]), at)
		}
	case "gemini": // [{pair:"BTCUSD", price, percentChange24h}] , prices only, no volume
		var rows []map[string]any
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			pair, _ := r["pair"].(string)
			base, quote := splitSuffix(pair, "USD")
			add(base, quote, num(r["price"]), 0, fetched)
		}
	default:
		return nil, errUnknownExchange(exchange)
	}
	if len(out) == 0 {
		return nil, errNoPairs(exchange)
	}
	return out, nil
}

type exchangeError string

func (e exchangeError) Error() string { return string(e) }

func errUnknownExchange(ex string) error { return exchangeError("unknown exchange " + ex) }
func errNoPairs(ex string) error         { return exchangeError(ex + ": no pair in the answer") }

// splitSuffix reads "SOLUSDT" into SOL/USDT and "SOLUSD" into SOL/USD (the longer quote first).
func splitSuffix(sym, prefer string) (base, quote string) {
	sym = strings.ToUpper(sym)
	for _, q := range []string{"USDT", "USD"} {
		if prefer == "USD" && q == "USDT" {
			continue // Gemini pairs are USD; "USDTUSD" is the stablecoin's own pair
		}
		if strings.HasSuffix(sym, q) && len(sym) > len(q) {
			return sym[:len(sym)-len(q)], q
		}
	}
	return "", ""
}

// krakenAssets are Kraken's own spellings for a few old coins.
var krakenAssets = map[string]string{"XBT": "BTC", "XDG": "DOGE", "XXBT": "BTC", "XETH": "ETH", "XXDG": "DOGE", "XXRP": "XRP", "XLTC": "LTC", "XXLM": "XLM", "XXMR": "XMR", "XETC": "ETC", "XZEC": "ZEC", "XMLN": "MLN", "XREP": "REP"}

// KrakenPair reads a Kraken result key ("XXBTZUSD", "SOLUSD", "USDTZUSD") into base and quote
// ("" when it is not a USD or USDT pair).
func KrakenPair(key string) (base, quote string) {
	key = strings.ToUpper(key)
	switch {
	case strings.HasSuffix(key, "ZUSD"):
		base, quote = key[:len(key)-4], "USD"
	case strings.HasSuffix(key, "USDT"):
		base, quote = key[:len(key)-4], "USDT"
	case strings.HasSuffix(key, "USD"):
		base, quote = key[:len(key)-3], "USD"
	default:
		return "", ""
	}
	if base == "" {
		return "", ""
	}
	if b, ok := krakenAssets[base]; ok {
		return b, quote
	}
	return base, quote
}

// bitfinexPair reads "tBTCUSD", "tSOLUST", "tUSTUSD" (UST is Bitfinex's USDT; a colon separates
// long names, "tMATIC:USD").
func bitfinexPair(sym string) (base, quote string) {
	if !strings.HasPrefix(sym, "t") {
		return "", ""
	}
	s := strings.ToUpper(sym[1:])
	if b, q, ok := strings.Cut(s, ":"); ok {
		base, quote = b, q
	} else {
		for _, q := range []string{"UST", "USD"} {
			if strings.HasSuffix(s, q) && len(s) > len(q) {
				base, quote = s[:len(s)-len(q)], q
				break
			}
		}
	}
	if quote == "UST" {
		quote = "USDT"
	}
	if base == "UST" {
		base = "USDT"
	}
	if quote != "USD" && quote != "USDT" {
		return "", ""
	}
	return base, quote
}

// Stablecoins and wrapped coins are quoted (USDT is the folding leg) but never constituents of the
// market index: a dollar token says nothing about crypto, and a wrapped coin is its coin twice.
var NotInIndex = map[string]bool{
	"USDT": true, "USDC": true, "DAI": true, "USDE": true, "FDUSD": true, "TUSD": true, "USDS": true, "PYUSD": true,
	"BUSD": true, "USD1": true, "USDD": true, "FRAX": true, "USDP": true, "GUSD": true, "EURC": true, "USDG": true, "RLUSD": true,
	"WBTC": true, "WETH": true, "STETH": true, "WSTETH": true, "WEETH": true, "CBBTC": true, "WBETH": true, "RETH": true, "LBTC": true,
	"BSC-USD": true, "SUSDE": true, "SUSDS": true, "BUIDL": true, "USDTB": true, "USYC": true,
}

// SymbolsFromRanks is the top n symbols of a rank list that can be constituents, in rank order.
func SymbolsFromRanks(coins []Coin, n int) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range coins {
		s := strings.ToUpper(c.Symbol)
		if s == "" || NotInIndex[s] || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= n {
			break
		}
	}
	return out
}
