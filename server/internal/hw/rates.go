package hw

import (
	"errors"
	"strconv"
	"strings"
)

// IndexRow is the box's USD price of one symbol, and how it was made.
type IndexRow struct {
	Price     float64 `json:"price"`
	At        int64   `json:"at"`
	N         int     `json:"n"`
	Spread    float64 `json:"spread"`
	Used      string  `json:"used"`
	Change24  float64 `json:"change24"`  // per cent against 24 hours before, from the hourly series
	HasChange bool    `json:"hasChange"` // false until the series reaches a day back
}

// RatesSnapshot is the box's market numbers as they stand: the ECB table of the newest day, the
// index per symbol followed (BTC, ETH, USDT…) and how each was made, and the newest rank list.
// Read by secd (/v1/rates) and by ghost.synthd for a money question; written by ghost.tallyd.
type RatesSnapshot struct {
	FXDay string              `json:"fxDay"` // "" when the box has no ECB table yet
	FX    map[string]float64  `json:"fx"`    // one euro in each currency
	Index map[string]IndexRow `json:"index"`
	// the BTC row's numbers again, flat, for the lines that only want bitcoin
	BTCUSD    float64   `json:"btcUsd"`
	BTCAt     int64     `json:"btcAt"`
	BTCN      int       `json:"btcN"`
	BTCSpread float64   `json:"btcSpread"`
	BTCUsed   string    `json:"btcUsed"`
	RanksAt   int64     `json:"ranksAt"`
	Ranks     []CoinRow `json:"ranks"`
	Source    string    `json:"ranksSource"`
	// FXDays and Days are how far back the tables go (the daily history)
	FXDays int `json:"fxDays"`
	Days   int `json:"days"`
}

// CoinRow is one line of the rank list.
type CoinRow struct {
	Rank      int     `json:"rank"`
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	PriceUSD  float64 `json:"priceUsd"`
	MarketCap float64 `json:"marketCap"`
	Change24  float64 `json:"change24"`
}

// USD is the index prices by symbol, the shape rates.Convert takes.
func (s RatesSnapshot) USD() map[string]float64 {
	out := map[string]float64{}
	for sym, r := range s.Index {
		if r.Price > 0 {
			out[sym] = r.Price
		}
	}
	return out
}

// RatesNow reads the snapshot over any connection. An empty box answers an empty snapshot and no
// error; a database that cannot be read is the error.
func RatesNow(c Querier) (RatesSnapshot, error) {
	s := RatesSnapshot{FX: map[string]float64{}, Index: map[string]IndexRow{}}
	rows, err := c.Query("SELECT day, code, rate FROM fx_rates WHERE day = (SELECT max(day) FROM fx_rates)")
	if err != nil {
		return s, err
	}
	for _, v := range rows.Vals {
		if len(v) < 3 || v[0] == nil || v[1] == nil || v[2] == nil {
			continue
		}
		s.FXDay = *v[0]
		if r, err := strconv.ParseFloat(*v[2], 64); err == nil {
			s.FX[*v[1]] = r
		}
	}
	rows, err = c.Query(`SELECT DISTINCT ON (symbol) symbol, ts, price, n, spread, used FROM crypto_index
		WHERE ts >= (SELECT coalesce(max(ts), 0) - 86400 FROM crypto_index) ORDER BY symbol, ts DESC`)
	if err != nil {
		return s, err
	}
	for _, v := range rows.Vals {
		if len(v) < 6 || v[0] == nil {
			continue
		}
		var r IndexRow
		r.At, _ = strconv.ParseInt(deref(v[1]), 10, 64)
		r.Price, _ = strconv.ParseFloat(deref(v[2]), 64)
		r.N, _ = strconv.Atoi(deref(v[3]))
		r.Spread, _ = strconv.ParseFloat(deref(v[4]), 64)
		r.Used = deref(v[5])
		s.Index[*v[0]] = r
	}
	// the change over 24 hours, from the hourly series
	newest := int64(0)
	for _, r := range s.Index {
		if r.At > newest {
			newest = r.At
		}
	}
	if newest > 0 {
		if rows, err := c.Query(`SELECT DISTINCT ON (symbol) symbol, close FROM crypto_series WHERE res = '1h' AND ts <= $1 AND ts >= $2 ORDER BY symbol, ts DESC`,
			newest-86400, newest-86400-7200); err == nil {
			for _, v := range rows.Vals {
				if len(v) != 2 || v[0] == nil || v[1] == nil {
					continue
				}
				old, _ := strconv.ParseFloat(*v[1], 64)
				if r, ok := s.Index[*v[0]]; ok && old > 0 && r.Price > 0 {
					r.Change24 = 100 * (r.Price/old - 1)
					r.HasChange = true
					s.Index[*v[0]] = r
				}
			}
		}
	}
	if b, ok := s.Index["BTC"]; ok {
		s.BTCUSD, s.BTCAt, s.BTCN, s.BTCSpread, s.BTCUsed = b.Price, b.At, b.N, b.Spread, b.Used
	}
	rows, err = c.Query(`SELECT ts, rank, symbol, name, price_usd, market_cap, change_24h, source FROM coin_ranks
		WHERE ts = (SELECT max(ts) FROM coin_ranks) ORDER BY rank LIMIT 100`)
	if err != nil {
		return s, err
	}
	for _, v := range rows.Vals {
		if len(v) < 8 {
			continue
		}
		s.RanksAt, _ = strconv.ParseInt(deref(v[0]), 10, 64)
		s.Source = deref(v[7])
		var r CoinRow
		r.Rank, _ = strconv.Atoi(deref(v[1]))
		r.Symbol, r.Name = deref(v[2]), deref(v[3])
		r.PriceUSD, _ = strconv.ParseFloat(deref(v[4]), 64)
		r.MarketCap, _ = strconv.ParseFloat(deref(v[5]), 64)
		r.Change24, _ = strconv.ParseFloat(deref(v[6]), 64)
		s.Ranks = append(s.Ranks, r)
	}
	if rows, err := c.Query("SELECT count(DISTINCT day) FROM fx_rates"); err == nil && len(rows.Vals) == 1 && rows.Vals[0][0] != nil {
		s.FXDays, _ = strconv.Atoi(*rows.Vals[0][0])
	}
	if rows, err := c.Query("SELECT count(DISTINCT day) FROM crypto_daily_index"); err == nil && len(rows.Vals) == 1 && rows.Vals[0][0] != nil {
		s.Days, _ = strconv.Atoi(*rows.Vals[0][0])
	}
	return s, nil
}

// DayPrice is one day of the daily history.
type DayPrice struct {
	Day   string  `json:"day"`
	Close float64 `json:"close"`
	N     int     `json:"n,omitempty"`
}

// RatesHistory is a symbol's daily USD closes (the box's daily index), or a currency's daily ECB
// rate (one euro in it), newest first, at most limit days.
func RatesHistory(c Querier, code string, limit int) ([]DayPrice, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, errors.New("code wanted")
	}
	if limit <= 0 || limit > 20000 {
		limit = 400
	}
	rows, err := c.Query("SELECT day, close, n FROM crypto_daily_index WHERE symbol = $1 ORDER BY day DESC LIMIT $2", code, limit)
	if err != nil {
		return nil, err
	}
	out := make([]DayPrice, 0, len(rows.Vals))
	for _, v := range rows.Vals {
		if len(v) < 3 || v[0] == nil {
			continue
		}
		var d DayPrice
		d.Day = *v[0]
		d.Close, _ = strconv.ParseFloat(deref(v[1]), 64)
		d.N, _ = strconv.Atoi(deref(v[2]))
		out = append(out, d)
	}
	if len(out) > 0 {
		return out, nil
	}
	rows, err = c.Query("SELECT day, rate FROM fx_rates WHERE code = $1 ORDER BY day DESC LIMIT $2", code, limit)
	if err != nil {
		return nil, err
	}
	for _, v := range rows.Vals {
		if len(v) < 2 || v[0] == nil {
			continue
		}
		var d DayPrice
		d.Day = *v[0]
		d.Close, _ = strconv.ParseFloat(deref(v[1]), 64)
		out = append(out, d)
	}
	return out, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// FXLine is the ECB table in a sentence for a prompt: the day and a few currencies, the ones
// asked for first.
func (s RatesSnapshot) FXLine(want ...string) string {
	if s.FXDay == "" || len(s.FX) == 0 {
		return ""
	}
	order := append([]string(nil), want...)
	for _, c := range []string{"USD", "GBP", "RON", "CHF", "JPY"} {
		found := false
		for _, w := range order {
			if w == c {
				found = true
			}
		}
		if !found {
			order = append(order, c)
		}
	}
	var parts []string
	for _, c := range order {
		if r, ok := s.FX[c]; ok && len(parts) < 6 {
			parts = append(parts, strconv.FormatFloat(r, 'f', 4, 64)+" "+c)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "ECB reference rates " + s.FXDay + ": 1 EUR = " + strings.Join(parts, ", ")
}

// ErrNoRates is what a converter says on an empty box.
var ErrNoRates = errors.New("the box has no rates yet (fetched hourly, by the phone on Wi-Fi or by the box)")
