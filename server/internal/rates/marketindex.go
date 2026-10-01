package rates

// THE MARKET INDEX: one number for crypto as a whole. The top fifty coins by market cap, weighted
// by their average daily dollar volume over the previous calendar month, the weights fixed for the
// month and set again at its start; the value chained day to day, so a change of constituents never
// jumps it. A coin with no price on a day is held at its last price (and named); the index starts at
// 1000 on the first day there is anything to price.

import (
	"math"
	"sort"
)

const (
	MarketIndexCode  = "CRYPTO50"
	MarketIndexSize  = 50
	MarketIndexStart = 1000.0
)

// Constituent is one coin's place in the index for a month.
type Constituent struct {
	Symbol    string  `json:"symbol"`
	Rank      int     `json:"rank"`      // by market cap at the month's start
	Weight    float64 `json:"weight"`    // of 1
	AvgVolume float64 `json:"avgVolume"` // average daily USD volume over the previous month
	BasePrice float64 `json:"basePrice"` // the close the month starts from
}

// Weights picks the constituents for a month: the n largest by market cap among the coins that
// have an average volume and a base price, each weighted by its share of the summed average
// volume. Stablecoins and wrapped coins are never in (NotInIndex).
func Weights(avgVolume, caps, basePrice map[string]float64, n int) []Constituent {
	type cand struct {
		sym string
		cap float64
	}
	var cands []cand
	for sym, c := range caps {
		if NotInIndex[sym] || c <= 0 || avgVolume[sym] <= 0 || basePrice[sym] <= 0 {
			continue
		}
		cands = append(cands, cand{sym, c})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].cap != cands[j].cap {
			return cands[i].cap > cands[j].cap
		}
		return cands[i].sym < cands[j].sym
	})
	if len(cands) > n {
		cands = cands[:n]
	}
	total := 0.0
	for _, c := range cands {
		total += avgVolume[c.sym]
	}
	if total <= 0 {
		return nil
	}
	out := make([]Constituent, 0, len(cands))
	for i, c := range cands {
		out = append(out, Constituent{Symbol: c.sym, Rank: i + 1, Weight: avgVolume[c.sym] / total, AvgVolume: avgVolume[c.sym], BasePrice: basePrice[c.sym]})
	}
	return out
}

// Value is the index on a day: the chain value times the weighted sum of each constituent's price
// over its base. A constituent with no price today takes its carried price (the last known), else
// its base (flat); both are named in missing.
func Value(chain float64, cons []Constituent, prices, carried map[string]float64) (value float64, priced int, missing []string) {
	if chain <= 0 || len(cons) == 0 {
		return 0, 0, nil
	}
	sum := 0.0
	for _, c := range cons {
		p := prices[c.Symbol]
		if p <= 0 {
			p = carried[c.Symbol]
			missing = append(missing, c.Symbol)
		} else {
			priced++
		}
		if p <= 0 {
			p = c.BasePrice
		}
		if c.BasePrice <= 0 || math.IsNaN(p) || math.IsInf(p, 0) {
			continue
		}
		sum += c.Weight * p / c.BasePrice
	}
	return chain * sum, priced, missing
}

// MonthOf is the YYYY-MM of a YYYY-MM-DD.
func MonthOf(day string) string {
	if len(day) < 7 {
		return day
	}
	return day[:7]
}
