package hw

// A COIN'S OWN PAGE: what it is (Coinbase's description, its site and white paper), where it
// stands on the list (rank, market cap, supply, the day's volume), the box's price and how it was
// blended right now, market by market (rates.Blend over the quotes of the last 25 minutes, each
// converted through the box's own price of its quote currency), and, for CRYPTO's rows, a week of
// hourly closes per coin (Sparks). The charts come from /v1/rates/series and /v1/rates/history.

import (
	"strconv"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/rates"
	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

// CoinDoc is /v1/coins/info.
type CoinDoc struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Rank        int    `json:"rank"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Website     string `json:"website"`
	WhitePaper  string `json:"whitepaper"`
	// what the box wrote about it from Wikipedia, the coin's own site and Coinbase's text, when, and from which
	Written     string `json:"written"`
	WrittenFrom string `json:"writtenFrom"`
	WrittenAt   int64  `json:"writtenAt"`
	// the list's (Coinbase's, hourly): its price, cap, circulating supply, 24-hour dollar volume and change
	ListPrice float64 `json:"listPrice"`
	MarketCap float64 `json:"marketCap"`
	Supply    float64 `json:"supply"`
	Volume24  float64 `json:"volume24"`
	Change24  float64 `json:"change24"`
	// the box's: its price as the last minute blended it, and each market's part right now
	Index   *IndexRow   `json:"index,omitempty"`
	Markets []rates.Leg `json:"markets"`
	At      int64       `json:"at"`
}

// CoinNow reads a coin's page.
func CoinNow(db *poltergres.ReadWrite, sym string, now time.Time) (CoinDoc, error) {
	sym = strings.ToUpper(strings.TrimSpace(sym))
	d := CoinDoc{Symbol: sym, Name: sym, Markets: []rates.Leg{}, At: now.Unix()}
	if rows, err := db.Query("SELECT name, description, color, website, whitepaper, written, written_from, written_at FROM coin_info WHERE symbol = $1", sym); err != nil {
		return d, err
	} else if len(rows.Vals) == 1 && len(rows.Vals[0]) == 8 {
		v := rows.Vals[0]
		if n := deref(v[0]); n != "" {
			d.Name = n
		}
		d.Description, d.Color, d.Website, d.WhitePaper = deref(v[1]), deref(v[2]), deref(v[3]), deref(v[4])
		d.Written, d.WrittenFrom = deref(v[5]), deref(v[6])
		d.WrittenAt, _ = strconv.ParseInt(deref(v[7]), 10, 64)
	}
	if rows, err := db.Query(`SELECT rank, name, price_usd, market_cap, volume_24h, change_24h, supply FROM coin_ranks
		WHERE symbol = $1 ORDER BY ts DESC LIMIT 1`, sym); err == nil && len(rows.Vals) == 1 && len(rows.Vals[0]) == 7 {
		v := rows.Vals[0]
		d.Rank, _ = strconv.Atoi(deref(v[0]))
		if n := deref(v[1]); n != "" && d.Name == sym {
			d.Name = n
		}
		d.ListPrice, _ = strconv.ParseFloat(deref(v[2]), 64)
		d.MarketCap, _ = strconv.ParseFloat(deref(v[3]), 64)
		d.Volume24, _ = strconv.ParseFloat(deref(v[4]), 64)
		d.Change24, _ = strconv.ParseFloat(deref(v[5]), 64)
		d.Supply, _ = strconv.ParseFloat(deref(v[6]), 64)
	}
	snap, err := RatesNow(db)
	if err != nil {
		return d, err
	}
	if r, ok := snap.Index[sym]; ok {
		d.Index = &r
	}
	// the blend right now: the newest quote of each market of the coin in the last 25 minutes,
	// each quote currency at the box's own price of it
	conv := map[string]float64{"USD": 1}
	if eur := tally.EURUSD(db); eur > 0 {
		conv["EUR"] = eur
	}
	for _, q := range []string{"USDT", "USDC", "BTC", "ETH"} {
		if r, ok := snap.Index[q]; ok && r.Price > 0 {
			conv[q] = r.Price
		} else if rates.Stable(q) {
			conv[q] = 1
		}
	}
	rows, err := db.Query(`SELECT DISTINCT ON (exchange, quote) exchange, quote, price, volume, quote_ts FROM crypto_quotes
		WHERE base = $1 AND ts >= $2 ORDER BY exchange, quote, ts DESC`, sym, now.Add(-25*time.Minute).Unix())
	if err != nil {
		return d, err
	}
	var qs []rates.Quote
	for _, v := range rows.Vals {
		if len(v) < 5 || v[0] == nil || v[1] == nil {
			continue
		}
		p, _ := strconv.ParseFloat(deref(v[2]), 64)
		vol, _ := strconv.ParseFloat(deref(v[3]), 64)
		at, _ := strconv.ParseInt(deref(v[4]), 10, 64)
		qs = append(qs, rates.Quote{Exchange: *v[0], Base: sym, QuoteCcy: *v[1], Price: p, Volume: vol, At: time.Unix(at, 0)})
	}
	prev := 0.0
	if d.Index != nil {
		prev = d.Index.Price
	}
	if _, legs, _ := rates.Blend(qs, sym, conv, prev, now); legs != nil {
		d.Markets = legs
	}
	return d, nil
}

// Sparks is each coin's hourly closes over the last hours (oldest first), for CRYPTO's rows.
func Sparks(c Querier, hours int, now time.Time) (map[string][]float64, error) {
	if hours <= 0 || hours > 24*30 {
		hours = 24 * 7
	}
	rows, err := c.Query(`SELECT symbol, close FROM crypto_series WHERE res = '1h' AND ts >= $1 ORDER BY symbol, ts`,
		now.Add(-time.Duration(hours)*time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	out := map[string][]float64{}
	for _, v := range rows.Vals {
		if len(v) < 2 || v[0] == nil || v[1] == nil {
			continue
		}
		f, _ := strconv.ParseFloat(*v[1], 64)
		if f > 0 {
			out[*v[0]] = append(out[*v[0]], f)
		}
	}
	return out, nil
}
