// Package rates reads the market data the phone fetched and turns it into the box's numbers:
// the ECB's daily reference rates (32 currencies against the euro, an offline converter), a BTC
// price from seven exchanges' public tickers (none needing a key), and the top 100 coins Coinbase
// lists, by market cap, from Coinbase itself. The phone fetches what it can on Wi-Fi, the box the
// rest (internal/egress); the parsing and the method live here and in ghost.tallyd.
package rates

import (
	"encoding/json"
	"encoding/xml"
	"errors"
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
// last 90 days, and the rank list. The tickers are not here: the box reads those itself every
// minute, whatever the phone is on, so the minute series has no gaps.
func PhoneSources() []Source {
	return []Source{
		{"ecb", "ECB reference rates", ECBDailyURL, 180},
		{"ecb-90d", "ECB, the last 90 days", ECB90DaysURL, 1440},
		{CoinbaseRanks, "Coinbase, the coins it lists by market cap", CoinbaseRanksURL, 60},
	}
}

// THE RANK LIST is Coinbase's: the coins it lists, largest market cap first, with each one's
// price, market cap, circulating supply, change over the day and dollar volume across the market.
// It is the list coinbase.com's own price pages read (not a documented API: Coinbase's documented
// public endpoints give products and volumes, and no market cap). The supply makes the cap at the
// box's own minute price, not the list's hourly one.
const (
	CoinbaseRanks    = "coinbase-ranks"
	CoinbaseRanksURL = "https://www.coinbase.com/api/v2/assets/search?base=USD&filter=listed&include_prices=true&resolution=day&sort=rank&order=asc&limit=100&page=1"
)

// RankSources are the rank lists' ids.
var RankSources = []string{CoinbaseRanks}

// IsRankSource says whether an id is a rank list.
func IsRankSource(id string) bool {
	for _, s := range RankSources {
		if s == id {
			return true
		}
	}
	return false
}

// TickerSources is the prices, every minute: every venue's all-pairs answer, one call each
// (Coinbase's products list among them). symbols is not needed for the asking: each answer holds
// every pair, and the ingest keeps the ones followed.
func TickerSources(symbols []string) []Source {
	_ = symbols
	var out []Source
	for _, ex := range BatchExchanges {
		out = append(out, Source{ex + ":all", ex + ", every pair", BatchURL(ex), 1})
	}
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
	Markets int               `json:"markets"` // markets that went in (a venue can have several: BTC-USD, BTC-EUR)
	Paths   []string          `json:"paths"`   // the quote currencies they were converted from (USD, USDT, BTC…)
	Spread  float64           `json:"spread"`  // (highest used - lowest used) / price
	Used    []string          `json:"used"`    // which
	Dropped map[string]string `json:"dropped"` // which, and why
	At      time.Time         `json:"at"`
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
	Supply    float64 `json:"supply"`   // circulating, in coins: the cap at any price is price × supply
}

// ParseCoins reads the rank list by source id: Coinbase's asset search.
func ParseCoins(source string, body []byte) ([]Coin, error) {
	switch source {
	case CoinbaseRanks: // {data:[{symbol, name, slug, rank, market_cap:"…", latest:"…", volume_24h:"…", percent_change: 0.0095}]}
		var obj struct {
			Data []struct {
				Slug   string `json:"slug"`
				Symbol string `json:"symbol"`
				Name   string `json:"name"`
				Rank   int    `json:"rank"`
				Cap    any    `json:"market_cap"`
				Price  any    `json:"latest"`
				Vol    any    `json:"volume_24h"`
				Change any    `json:"percent_change"` // a fraction of one: 0.0095 is +0.95%
				Supply any    `json:"circulating_supply"`
				Listed *bool  `json:"listed"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &obj); err != nil {
			return nil, err
		}
		out := make([]Coin, 0, len(obj.Data))
		for _, r := range obj.Data {
			if r.Listed != nil && !*r.Listed {
				continue
			}
			out = append(out, Coin{r.Rank, r.Slug, strings.ToUpper(r.Symbol), r.Name, anyNum(r.Price), anyNum(r.Cap), anyNum(r.Vol), 100 * anyNum(r.Change), anyNum(r.Supply)})
		}
		return finishCoins(out)
	}
	return nil, errors.New("unknown rank source " + source)
}

// CoinInfo is a coin as Coinbase's list describes it, for the coin's own page.
type CoinInfo struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Website     string `json:"website"`
	WhitePaper  string `json:"whitepaper"`
}

// ParseCoinInfo reads the descriptions out of Coinbase's list (the same body as ParseCoins).
func ParseCoinInfo(body []byte) []CoinInfo {
	var obj struct {
		Data []struct {
			Symbol      string `json:"symbol"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Color       string `json:"color"`
			Listed      *bool  `json:"listed"`
			URLs        []struct {
				Type string `json:"type"`
				Link string `json:"link"`
			} `json:"resource_urls"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &obj) != nil {
		return nil
	}
	var out []CoinInfo
	for _, r := range obj.Data {
		if r.Symbol == "" || (r.Listed != nil && !*r.Listed) {
			continue
		}
		ci := CoinInfo{Symbol: strings.ToUpper(r.Symbol), Name: r.Name, Description: strings.TrimSpace(r.Description)}
		if len(r.Color) == 7 && r.Color[0] == '#' {
			ci.Color = r.Color
		}
		for _, u := range r.URLs {
			if !strings.HasPrefix(u.Link, "https://") {
				continue
			}
			switch u.Type {
			case "website":
				ci.Website = u.Link
			case "white_paper":
				ci.WhitePaper = u.Link
			}
		}
		out = append(out, ci)
	}
	return out
}

// anyNum reads a number Coinbase sends as a string or as a number.
func anyNum(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
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
