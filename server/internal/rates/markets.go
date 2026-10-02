package rates

// THE MARKETS: which exchange pairs the box reads for a symbol, how each exchange is asked for a
// ticker and for daily candles, and how each answers. Seven venues, all public, no key: Coinbase,
// Kraken, Bitstamp, Gemini, Binance, Bitfinex, OKX. Binance and OKX quote in USDT; those prices
// are folded into USD with the USDT/USD rate the USD venues give for USDT itself, so the index
// is one USD number per symbol.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Market is one exchange's pair.
type Market struct {
	Exchange string `json:"exchange"`
	Base     string `json:"base"`
	Quote    string `json:"quote"` // USD or USDT
}

// ID is the market's name in the sources list and the marks: "coinbase:BTC-USD".
func (m Market) ID() string { return m.Exchange + ":" + m.Base + "-" + m.Quote }

// ParseMarketID reads an ID back.
func ParseMarketID(id string) (Market, bool) {
	i := strings.IndexByte(id, ':')
	j := strings.IndexByte(id, '-')
	if i <= 0 || j <= i+1 || j >= len(id)-1 {
		return Market{}, false
	}
	return Market{Exchange: id[:i], Base: id[i+1 : j], Quote: id[j+1:]}, true
}

// Exchanges, in the order the index names them.
var Exchanges = []string{"binance", "bitfinex", "bitstamp", "coinbase", "gemini", "kraken", "okx"}

// DefaultSymbols is what a new box follows; `ghost-cli ghost.tallyd rates add=SOL` extends it.
var DefaultSymbols = []string{"BTC", "ETH"}

// Markets is every pair to read for the symbols: a USD pair on the USD venues, a USDT pair on
// Binance and OKX, and the USDT/USD legs that fold the latter into dollars.
func Markets(symbols []string) []Market {
	var out []Market
	seen := map[string]bool{}
	add := func(m Market) {
		if !seen[m.ID()] {
			seen[m.ID()] = true
			out = append(out, m)
		}
	}
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" || s == "USDT" || s == "USD" {
			continue
		}
		for _, ex := range []string{"coinbase", "kraken", "bitstamp", "gemini", "bitfinex"} {
			add(Market{ex, s, "USD"})
		}
		for _, ex := range []string{"binance", "okx"} {
			add(Market{ex, s, "USDT"})
		}
	}
	for _, ex := range []string{"coinbase", "kraken", "bitstamp", "bitfinex"} {
		add(Market{ex, "USDT", "USD"})
	}
	return out
}

// exchangeSymbol is the pair as the venue spells it.
func exchangeSymbol(m Market) string {
	base, quote := m.Base, m.Quote
	switch m.Exchange {
	case "coinbase":
		return base + "-" + quote
	case "kraken":
		if base == "BTC" {
			base = "XBT"
		}
		return base + quote
	case "bitstamp", "gemini":
		return strings.ToLower(base + quote)
	case "binance":
		return base + quote
	case "bitfinex":
		if base == "USDT" {
			base = "UST"
		}
		if quote == "USDT" {
			quote = "UST"
		}
		return "t" + base + quote
	case "okx":
		return base + "-" + quote
	}
	return base + quote
}

// TickerURL is the venue's public ticker for the pair.
func (m Market) TickerURL() string {
	p := exchangeSymbol(m)
	switch m.Exchange {
	case "coinbase":
		return "https://api.exchange.coinbase.com/products/" + p + "/ticker"
	case "kraken":
		return "https://api.kraken.com/0/public/Ticker?pair=" + p
	case "bitstamp":
		return "https://www.bitstamp.net/api/v2/ticker/" + p + "/"
	case "gemini":
		return "https://api.gemini.com/v1/pubticker/" + p
	case "binance":
		return "https://api.binance.com/api/v3/ticker/24hr?symbol=" + p
	case "bitfinex":
		return "https://api-pub.bitfinex.com/v2/ticker/" + p
	case "okx":
		return "https://www.okx.com/api/v5/market/ticker?instId=" + p
	}
	return ""
}

// CandlesURL is the venue's daily candles from a day on (Coinbase and Kraken to an end; the
// others take a start and a count). Each venue pages differently; the box walks back a page a
// tick and stops when a page comes back empty.
func (m Market) CandlesURL(from, to time.Time) string { return m.BarsURL(24*time.Hour, from, to) }

// HourlyURL is the venue's hourly candles between two times.
func (m Market) HourlyURL(from, to time.Time) string { return m.BarsURL(time.Hour, from, to) }

// MinuteURL is the venue's one-minute candles between two times.
func (m Market) MinuteURL(from, to time.Time) string { return m.BarsURL(time.Minute, from, to) }

// BarsURL is the venue's candles of one size (a day, an hour or a minute) between two times.
func (m Market) BarsURL(bar time.Duration, from, to time.Time) string {
	p := exchangeSymbol(m)
	sec := int(bar.Seconds())
	pick := func(day, hour string) string {
		switch bar {
		case time.Hour:
			return hour
		case time.Minute:
			// the same spellings with the hour's unit swapped for the minute's
			return strings.NewReplacer("1hr", "1m", "1H", "1m", "1h", "1m").Replace(hour)
		}
		return day
	}
	switch m.Exchange {
	case "coinbase": // at most 300 candles between start and end
		return fmt.Sprintf("https://api.exchange.coinbase.com/products/%s/candles?granularity=%d&start=%s&end=%s", p, sec, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	case "kraken": // the last 720 bars from since, whatever the end
		return fmt.Sprintf("https://api.kraken.com/0/public/OHLC?pair=%s&interval=%d&since=%d", p, sec/60, from.Unix())
	case "bitstamp":
		return fmt.Sprintf("https://www.bitstamp.net/api/v2/ohlc/%s/?step=%d&limit=1000&start=%d", p, sec, from.Unix())
	case "gemini": // no paging: what the venue keeps
		return fmt.Sprintf("https://api.gemini.com/v2/candles/%s/%s", p, pick("1day", "1hr"))
	case "binance":
		return fmt.Sprintf("https://api.binance.com/api/v3/klines?symbol=%s&interval=%s&startTime=%d&endTime=%d&limit=1000", p, pick("1d", "1h"), from.UnixMilli(), to.UnixMilli())
	case "bitfinex":
		return fmt.Sprintf("https://api-pub.bitfinex.com/v2/candles/trade:%s:%s/hist?start=%d&end=%d&limit=1000&sort=1", pick("1D", "1h"), p, from.UnixMilli(), to.UnixMilli())
	case "okx": // at most 100 a call, older than `after`
		return fmt.Sprintf("https://www.okx.com/api/v5/market/history-candles?instId=%s&bar=%s&after=%d&limit=100", p, pick("1D", "1H"), to.UnixMilli())
	}
	return ""
}

// PageDays is how many days one daily candles call covers at most, for walking back.
func (m Market) PageDays() int { return m.PageBars() }

// PageBars is how many bars one candles call covers at most, whatever their size.
func (m Market) PageBars() int {
	switch m.Exchange {
	case "coinbase":
		return 300
	case "okx":
		return 100
	case "kraken":
		return 720
	}
	return 1000
}

// ParseTicker reads one venue's ticker body for a market. fetched is when it was fetched.
func ParseTicker(m Market, body []byte, fetched time.Time) (Quote, error) {
	q := Quote{Exchange: m.Exchange, Base: m.Base, QuoteCcy: m.Quote, At: fetched}
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
	switch m.Exchange {
	case "bitfinex":
		var arr []any
		if err := json.Unmarshal(body, &arr); err != nil || len(arr) < 8 {
			return q, errors.New("bitfinex: not a ticker")
		}
		q.Price, q.Volume = num(arr[6]), num(arr[7])
	default:
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			return q, err
		}
		switch m.Exchange {
		case "coinbase":
			q.Price, q.Volume = num(obj["price"]), num(obj["volume"])
			if t, ok := obj["time"].(string); ok {
				if at, err := time.Parse(time.RFC3339Nano, t); err == nil {
					q.At = at
				}
			}
		case "kraken":
			res, _ := obj["result"].(map[string]any)
			for k, v := range res {
				if k == "last" {
					continue
				}
				pair, _ := v.(map[string]any)
				if c, ok := pair["c"].([]any); ok && len(c) > 0 {
					q.Price = num(c[0])
				}
				if vol, ok := pair["v"].([]any); ok && len(vol) > 1 {
					q.Volume = num(vol[1])
				}
			}
		case "bitstamp":
			q.Price, q.Volume = num(obj["last"]), num(obj["volume"])
			if ts := num(obj["timestamp"]); ts > 0 {
				q.At = time.Unix(int64(ts), 0)
			}
		case "gemini":
			q.Price = num(obj["last"])
			if vol, ok := obj["volume"].(map[string]any); ok {
				q.Volume = num(vol[m.Base])
				if ts := num(vol["timestamp"]); ts > 0 {
					q.At = time.UnixMilli(int64(ts))
				}
			}
		case "binance":
			q.Price, q.Volume = num(obj["lastPrice"]), num(obj["volume"])
			if ts := num(obj["closeTime"]); ts > 0 {
				q.At = time.UnixMilli(int64(ts))
			}
		case "okx":
			data, _ := obj["data"].([]any)
			if len(data) > 0 {
				row, _ := data[0].(map[string]any)
				q.Price, q.Volume = num(row["last"]), num(row["vol24h"])
				if ts := num(row["ts"]); ts > 0 {
					q.At = time.UnixMilli(int64(ts))
				}
			}
		default:
			return q, errors.New("unknown exchange " + m.Exchange)
		}
	}
	if q.Price <= 0 {
		return q, errors.New(m.Exchange + ": no price in the answer")
	}
	return q, nil
}

// Candle is one day of one market.
type Candle struct {
	Day                            string // YYYY-MM-DD, UTC
	Open, High, Low, Close, Volume float64
}

// Bar is one candle of any size: its start, in unix seconds.
type Bar struct {
	TS                             int64
	Open, High, Low, Close, Volume float64
}

// ParseCandles reads one venue's daily candles. Days with no close are dropped; the newest first.
func ParseCandles(m Market, body []byte) ([]Candle, error) {
	bars, err := ParseBars(m, body)
	if err != nil {
		return nil, err
	}
	out := make([]Candle, 0, len(bars))
	for _, b := range bars {
		out = append(out, Candle{time.Unix(b.TS, 0).UTC().Format("2006-01-02"), b.Open, b.High, b.Low, b.Close, b.Volume})
	}
	return out, nil
}

// ParseBars reads one venue's candles of any size. Bars with no close are dropped; the newest first.
func ParseBars(m Market, body []byte) ([]Bar, error) {
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
	var out []Bar
	switch m.Exchange {
	case "coinbase": // [time, low, high, open, close, volume]
		var rows [][]any
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if len(r) >= 6 {
				out = append(out, Bar{int64(num(r[0])), num(r[3]), num(r[2]), num(r[1]), num(r[4]), num(r[5])})
			}
		}
	case "kraken": // result.<pair>: [time, open, high, low, close, vwap, volume, count]
		var obj struct {
			Result map[string]json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(body, &obj); err != nil {
			return nil, err
		}
		for k, raw := range obj.Result {
			if k == "last" {
				continue
			}
			var rows [][]any
			if json.Unmarshal(raw, &rows) != nil {
				continue
			}
			for _, r := range rows {
				if len(r) >= 7 {
					out = append(out, Bar{int64(num(r[0])), num(r[1]), num(r[2]), num(r[3]), num(r[4]), num(r[6])})
				}
			}
		}
	case "bitstamp": // data.ohlc: [{timestamp, open, high, low, close, volume}]
		var obj struct {
			Data struct {
				OHLC []map[string]any `json:"ohlc"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &obj); err != nil {
			return nil, err
		}
		for _, r := range obj.Data.OHLC {
			out = append(out, Bar{int64(num(r["timestamp"])), num(r["open"]), num(r["high"]), num(r["low"]), num(r["close"]), num(r["volume"])})
		}
	case "gemini": // [time_ms, open, high, low, close, volume]
		var rows [][]any
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if len(r) >= 6 {
				out = append(out, Bar{int64(num(r[0])) / 1000, num(r[1]), num(r[2]), num(r[3]), num(r[4]), num(r[5])})
			}
		}
	case "binance": // [openTime, open, high, low, close, volume, closeTime, ...]
		var rows [][]any
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if len(r) >= 6 {
				out = append(out, Bar{int64(num(r[0])) / 1000, num(r[1]), num(r[2]), num(r[3]), num(r[4]), num(r[5])})
			}
		}
	case "bitfinex": // [MTS, OPEN, CLOSE, HIGH, LOW, VOLUME]
		var rows [][]any
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if len(r) >= 6 {
				out = append(out, Bar{int64(num(r[0])) / 1000, num(r[1]), num(r[3]), num(r[4]), num(r[2]), num(r[5])})
			}
		}
	case "okx": // data: [[ts, o, h, l, c, vol, ...]] as strings
		var obj struct {
			Data [][]any `json:"data"`
		}
		if err := json.Unmarshal(body, &obj); err != nil {
			return nil, err
		}
		for _, r := range obj.Data {
			if len(r) >= 6 {
				out = append(out, Bar{int64(num(r[0])) / 1000, num(r[1]), num(r[2]), num(r[3]), num(r[4]), num(r[5])})
			}
		}
	default:
		return nil, errors.New("unknown exchange " + m.Exchange)
	}
	kept := out[:0]
	for _, b := range out {
		if b.Close > 0 && b.TS > 1230768000 { // 2009
			kept = append(kept, b)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].TS > kept[j].TS })
	if len(kept) == 0 {
		return nil, errors.New(m.Exchange + ": no candles in the answer")
	}
	return kept, nil
}

// FoldBar is one bar of the box's price for a symbol from the venues' bars at the same time: each
// of open, high, low and close is the mean of the venues' within 5% of their median, USDT bars
// folded into dollars with the USDT/USD rate of that time (1 when there is none). n is how many
// venues went in.
func FoldBar(bars map[Market]Bar, symbol string, usdt float64) (Bar, int) {
	if usdt <= 0 {
		usdt = 1
	}
	var o, h, l, c []float64
	var vol float64
	var ts int64
	for m, b := range bars {
		if m.Base != symbol || b.Close <= 0 {
			continue
		}
		f := 1.0
		if m.Quote == "USDT" {
			f = usdt
		}
		o, h, l, c = append(o, b.Open*f), append(h, b.High*f), append(l, b.Low*f), append(c, b.Close*f)
		vol += b.Volume
		ts = b.TS
	}
	if len(c) == 0 {
		return Bar{}, 0
	}
	cl, n := filteredMean(c)
	op, _ := filteredMean(o)
	hi, _ := filteredMean(h)
	lo, _ := filteredMean(l)
	if op <= 0 {
		op = cl
	}
	if hi < cl || hi <= 0 {
		hi = math.Max(cl, op)
	}
	if lo <= 0 || lo > cl {
		lo = math.Min(cl, op)
	}
	return Bar{TS: ts, Open: op, High: hi, Low: lo, Close: cl, Volume: vol}, n
}

// filteredMean is the mean of the values within 5% of their median, and how many.
func filteredMean(vals []float64) (float64, int) {
	var ps []float64
	for _, v := range vals {
		if v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			ps = append(ps, v)
		}
	}
	if len(ps) == 0 {
		return 0, 0
	}
	sort.Float64s(ps)
	med := ps[len(ps)/2]
	if len(ps)%2 == 0 {
		med = (ps[len(ps)/2-1] + ps[len(ps)/2]) / 2
	}
	sum, n := 0.0, 0
	for _, p := range ps {
		if math.Abs(p-med)/med <= 0.05 {
			sum += p
			n++
		}
	}
	if n == 0 {
		return med, len(ps)
	}
	return sum / float64(n), n
}

// DailyIndex is one day's USD close for a symbol from the venues' closes: the median, USDT closes
// folded with the day's USDT/USD close (1 when the day has none).
func DailyIndex(closes map[Market]float64, symbol string, usdtClose float64) (float64, int) {
	if usdtClose <= 0 {
		usdtClose = 1
	}
	var ps []float64
	for m, c := range closes {
		if m.Base != symbol || c <= 0 {
			continue
		}
		if m.Quote == "USDT" {
			c *= usdtClose
		}
		ps = append(ps, c)
	}
	if len(ps) == 0 {
		return 0, 0
	}
	sort.Float64s(ps)
	// a venue a long way from the rest is a bad print, not a price
	med := ps[len(ps)/2]
	if len(ps)%2 == 0 {
		med = (ps[len(ps)/2-1] + ps[len(ps)/2]) / 2
	}
	var kept []float64
	for _, p := range ps {
		if math.Abs(p-med)/med <= 0.05 {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return med, len(ps)
	}
	sum := 0.0
	for _, p := range kept {
		sum += p
	}
	return sum / float64(len(kept)), len(kept)
}
