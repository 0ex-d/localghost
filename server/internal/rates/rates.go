// Package rates reads the market data the phone fetched and turns it into the box's numbers:
// the ECB's daily reference rates (32 currencies against the euro, an offline converter), a BTC
// price from four public exchange tickers (Coinbase, Kraken, Bitstamp, Gemini, none needing a
// key), and the top 100 coins from CoinGecko with CoinPaprika as the fallback. The box never
// opens a connection: the phone fetches each body and hands it over; the parsing and the method
// live here and in ghost.tallyd.
package rates

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Source is one address to fetch, by the phone when it is on Wi-Fi, by the box otherwise.
type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	// Every is how often it wants fetching, in minutes (the ECB publishes once a day around 16:00
	// CET; the tickers move all the time; the rank lists and the candles need no more than hourly
	// and daily).
	Every int `json:"every"`
}

const (
	ECBDailyURL   = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"
	ECB90DaysURL  = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-hist-90d.xml"
	ECBHistoryURL = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-hist.xml" // every day since 1999, 7 MB: the box, once
)

// Sources is everything to fetch for the symbols followed: the tickers and the rest.
func Sources(symbols []string, now time.Time) []Source {
	return append(PhoneSources(), TickerSources(symbols)...)
}

// PhoneSources is what the phone fetches on Wi-Fi and the box otherwise: the ECB's day and its
// last 90 days, and the two rank lists. The tickers are not here: the box reads those itself
// every minute, whatever the phone is on, so the minute series has no gaps.
func PhoneSources() []Source {
	return []Source{
		{"ecb", "ECB reference rates", ECBDailyURL, 180},
		{"ecb-90d", "ECB, the last 90 days", ECB90DaysURL, 1440},
		{"coingecko", "CoinGecko", "https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&order=market_cap_desc&per_page=100&page=1", 60},
		{"coinpaprika", "CoinPaprika", "https://api.coinpaprika.com/v1/tickers?quotes=USD&limit=100", 60},
	}
}

// TickerSources is the prices, every minute: every batch venue's all-pairs ticker (one call
// each) and Coinbase pair by pair for the ten largest symbols and the USDT leg.
func TickerSources(symbols []string) []Source {
	var out []Source
	for _, ex := range BatchExchanges {
		out = append(out, Source{ex + ":all", ex + ", every pair", BatchURL(ex), 1})
	}
	n := 0
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" || s == "USDT" || s == "USD" {
			continue
		}
		m := Market{"coinbase", s, "USD"}
		out = append(out, Source{m.ID(), "coinbase " + s + "/USD", m.TickerURL(), 1})
		if n++; n >= CoinbaseTop {
			break
		}
	}
	m := Market{"coinbase", "USDT", "USD"}
	out = append(out, Source{m.ID(), "coinbase USDT/USD", m.TickerURL(), 1})
	return out
}

// DefaultSources is Sources for the default symbols, now.
func DefaultSources() []Source { return Sources(DefaultSymbols, time.Now()) }

// --- the ECB -------------------------------------------------------------------------------

// ECBDay is one day's table: one euro in each currency.
type ECBDay struct {
	Day   string
	Rates map[string]float64
}

// ParseECBAll reads any of the ECB's eurofxref files: the daily one (one day), the 90-day one,
// the full history. Newest first.
func ParseECBAll(body []byte) ([]ECBDay, error) {
	var env struct {
		Cube struct {
			Days []struct {
				Time  string `xml:"time,attr"`
				Rates []struct {
					Currency string `xml:"currency,attr"`
					Rate     string `xml:"rate,attr"`
				} `xml:"Cube"`
			} `xml:"Cube"`
		} `xml:"Cube"`
	}
	d := xml.NewDecoder(strings.NewReader(string(body)))
	d.Strict = false
	if err := d.Decode(&env); err != nil {
		return nil, err
	}
	var out []ECBDay
	for _, dc := range env.Cube.Days {
		if _, err := time.Parse("2006-01-02", dc.Time); err != nil {
			continue
		}
		rates := map[string]float64{}
		for _, r := range dc.Rates {
			v, err := strconv.ParseFloat(r.Rate, 64)
			if err != nil || v <= 0 || len(r.Currency) != 3 {
				continue
			}
			rates[strings.ToUpper(r.Currency)] = v
		}
		if len(rates) >= 10 {
			out = append(out, ECBDay{dc.Time, rates})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no rates in the ECB file")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day > out[j].Day })
	return out, nil
}

// ParseECB is the newest day of an ECB file.
func ParseECB(body []byte) (day string, rates map[string]float64, err error) {
	days, err := ParseECBAll(body)
	if err != nil {
		return "", nil, err
	}
	return days[0].Day, days[0].Rates, nil
}

// --- BTC -----------------------------------------------------------------------------------

// Quote is one venue's last price and 24-hour volume in the base. At is the venue's own time when
// it gives one, else the time it was fetched.
type Quote struct {
	Exchange string    `json:"exchange"`
	Base     string    `json:"base"`
	QuoteCcy string    `json:"quote"`
	Price    float64   `json:"price"`
	Volume   float64   `json:"volume"`
	At       time.Time `json:"at"`
}

// Index is the box's USD price of one symbol and how it was made.
type Index struct {
	Price   float64           `json:"price"`
	N       int               `json:"n"`       // exchanges that went in
	Spread  float64           `json:"spread"`  // (highest used - lowest used) / price
	Used    []string          `json:"used"`    // which
	Dropped map[string]string `json:"dropped"` // which, and why
	At      time.Time         `json:"at"`
}

const (
	staleAfter = 15 * time.Minute
	maxOff     = 0.02 // from the median
)

// MakeIndex is the method: quotes older than fifteen minutes go, then quotes more than 2% from the
// median of the rest, and what is left is averaged weighted by volume (plain mean when no
// exchange gives a volume). The spread is told so a thin or disagreeing market shows.
func MakeIndex(quotes []Quote, now time.Time) (Index, error) {
	ix := Index{Dropped: map[string]string{}, At: now}
	var fresh []Quote
	for _, q := range quotes {
		switch {
		case q.Price <= 0:
			ix.Dropped[q.Exchange] = "no price"
		case now.Sub(q.At) > staleAfter:
			ix.Dropped[q.Exchange] = "stale (" + now.Sub(q.At).Truncate(time.Minute).String() + " old)"
		default:
			fresh = append(fresh, q)
		}
	}
	if len(fresh) == 0 {
		return ix, errors.New("no fresh quote")
	}
	prices := make([]float64, len(fresh))
	for i, q := range fresh {
		prices[i] = q.Price
	}
	sort.Float64s(prices)
	median := prices[len(prices)/2]
	if len(prices)%2 == 0 {
		median = (prices[len(prices)/2-1] + prices[len(prices)/2]) / 2
	}
	var sumPV, sumV float64
	var used []Quote
	anyVolume := false
	for _, q := range fresh {
		if q.Volume > 0 && math.Abs(q.Price-median)/median <= maxOff {
			anyVolume = true
		}
	}
	for _, q := range fresh {
		if math.Abs(q.Price-median)/median > maxOff {
			ix.Dropped[q.Exchange] = "off by " + strconv.FormatFloat(100*math.Abs(q.Price-median)/median, 'f', 1, 64) + "% from the median"
			continue
		}
		if anyVolume && q.Volume <= 0 {
			// a venue that gives a price and no volume (Gemini's price feed) cannot be weighed
			// against the ones that do; it is left out rather than counted for nothing
			ix.Dropped[q.Exchange] = "no volume given"
			continue
		}
		used = append(used, q)
		sumPV += q.Price * q.Volume
		sumV += q.Volume
	}
	if len(used) == 0 {
		return ix, errors.New("every quote was off the median")
	}
	lo, hi := used[0].Price, used[0].Price
	for _, q := range used {
		ix.Used = append(ix.Used, q.Exchange)
		lo, hi = math.Min(lo, q.Price), math.Max(hi, q.Price)
	}
	sort.Strings(ix.Used)
	if sumV > 0 {
		ix.Price = sumPV / sumV
	} else {
		for _, q := range used {
			ix.Price += q.Price
		}
		ix.Price /= float64(len(used))
	}
	ix.N = len(used)
	ix.Spread = (hi - lo) / ix.Price
	return ix, nil
}

// --- the top 100 ---------------------------------------------------------------------------

// Coin is one row of the rank list.
type Coin struct {
	Rank      int     `json:"rank"`
	ID        string  `json:"id"`
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	PriceUSD  float64 `json:"priceUsd"`
	MarketCap float64 `json:"marketCap"`
	Volume24  float64 `json:"volume24"`
	Change24  float64 `json:"change24"` // per cent
}

// ParseCoins reads CoinGecko's /coins/markets or CoinPaprika's /tickers, by source id.
func ParseCoins(source string, body []byte) ([]Coin, error) {
	switch source {
	case "coingecko":
		var rows []struct {
			ID     string  `json:"id"`
			Symbol string  `json:"symbol"`
			Name   string  `json:"name"`
			Price  float64 `json:"current_price"`
			Cap    float64 `json:"market_cap"`
			Rank   int     `json:"market_cap_rank"`
			Vol    float64 `json:"total_volume"`
			Change float64 `json:"price_change_percentage_24h"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		out := make([]Coin, 0, len(rows))
		for i, r := range rows {
			rank := r.Rank
			if rank == 0 {
				rank = i + 1
			}
			out = append(out, Coin{rank, r.ID, strings.ToUpper(r.Symbol), r.Name, r.Price, r.Cap, r.Vol, r.Change})
		}
		return finishCoins(out)
	case "coinpaprika":
		var rows []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Symbol string `json:"symbol"`
			Rank   int    `json:"rank"`
			Quotes struct {
				USD struct {
					Price  float64 `json:"price"`
					Vol    float64 `json:"volume_24h"`
					Cap    float64 `json:"market_cap"`
					Change float64 `json:"percent_change_24h"`
				} `json:"USD"`
			} `json:"quotes"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		out := make([]Coin, 0, len(rows))
		for _, r := range rows {
			out = append(out, Coin{r.Rank, r.ID, strings.ToUpper(r.Symbol), r.Name, r.Quotes.USD.Price, r.Quotes.USD.Cap, r.Quotes.USD.Vol, r.Quotes.USD.Change})
		}
		return finishCoins(out)
	}
	return nil, errors.New("unknown rank source " + source)
}

func finishCoins(in []Coin) ([]Coin, error) {
	out := in[:0]
	for _, c := range in {
		if c.Rank > 0 && c.PriceUSD > 0 && c.Symbol != "" {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	if len(out) > 100 {
		out = out[:100]
	}
	if len(out) < 10 {
		return nil, errors.New("too few coins in the list")
	}
	return out, nil
}

// --- the converter ---------------------------------------------------------------------------

// Convert turns amount in from into to, through the euro: fx is the ECB table (one euro in each
// currency; EUR itself need not be present), usd the box's USD prices by symbol (BTC, ETH, USDT…).
// Codes are ISO letters or a symbol the box follows.
func Convert(amount float64, from, to string, fx map[string]float64, usd map[string]float64) (float64, error) {
	eurPer := func(code string) (float64, error) {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "XBT" {
			code = "BTC"
		}
		if code == "EUR" {
			return 1, nil
		}
		if r, ok := fx[code]; ok && r > 0 {
			return 1 / r, nil
		}
		if p, ok := usd[code]; ok && p > 0 {
			usdRate, ok := fx["USD"]
			if !ok || usdRate <= 0 {
				return 0, errors.New("no USD rate yet")
			}
			return p / usdRate, nil
		}
		return 0, errors.New("no rate for " + code)
	}
	a, err := eurPer(from)
	if err != nil {
		return 0, err
	}
	b, err := eurPer(to)
	if err != nil {
		return 0, err
	}
	return amount * a / b, nil
}
