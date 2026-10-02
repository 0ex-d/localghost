package rates

// THE BOX'S PRICE, A BLEND OF EVERY MARKET. One coin's dollar price from every market it trades
// in: each market's last price converted to dollars through its quote currency (a USD market as
// it is, a USDT, USDC, EUR, BTC or ETH market through that currency's own price, blended the same
// way first), weighted by the market's 24-hour volume in the coin itself and by how fresh the
// price is, with a market far off the coin's last price left out:
//
//	P = Σ w_m · p_m · FX_q        w_m ∝ (1/ε_m) · V_m · γ_m
//
//	γ (time penalty): 1 for a price under 60 s old, falling in a straight line to 0.001 at 1,500 s
//	ε (outlier): with more than two markets, a price above A × the last value, or below it / A,
//	  is out; A is 1.05 with 15 markets or more, 1.10 with 10 to 14, 1.15 with fewer
//	V: the market's 24-hour volume in the coin (so every market of a coin is weighed in one unit)
//
// The box reads tickers (each venue's last price and 24-hour volume, once a minute and every five
// seconds for BTC, ETH and SOL), not every trade, so a market's price is its ticker's. A stablecoin
// is priced from its USD and USDT markets only, so a dollar is not priced through itself. When no
// market gives a volume (Gemini's feed alone), every market weighs the same.

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ConvQuotes are the currencies a market's price is converted from, in the order their own
// prices are worked out: USDT and USDC from USD markets, EUR from the ECB, then BTC and ETH.
var ConvQuotes = []string{"USD", "USDT", "USDC", "EUR", "BTC", "ETH"}

// IsConvQuote says whether a price in q can be converted to dollars.
func IsConvQuote(q string) bool {
	for _, c := range ConvQuotes {
		if c == q {
			return true
		}
	}
	return false
}

// Stable is a dollar or euro token: priced from USD and USDT markets only.
func Stable(sym string) bool {
	switch sym {
	case "USDT", "USDC", "DAI", "USDE", "FDUSD", "TUSD", "USDS", "PYUSD", "BUSD", "USD1", "USDD", "FRAX",
		"USDP", "GUSD", "USDG", "RLUSD", "BSC-USD", "USDTB", "EURC":
		return true
	}
	return false
}

const (
	gammaMin = 0.001
	tauMin   = 60.0   // seconds: fresher than this, no penalty
	tauMax   = 1500.0 // seconds: older than this, the floor
)

// TimePenalty is γ for a price this old.
func TimePenalty(age time.Duration) float64 {
	t := age.Seconds()
	switch {
	case t <= tauMin:
		return 1
	case t >= tauMax:
		return gammaMin
	}
	return 1 + (gammaMin-1)/(tauMax-tauMin)*(t-tauMin)
}

// outlierBand is A for this many markets.
func outlierBand(n int) float64 {
	switch {
	case n >= 15:
		return 1.05
	case n >= 10:
		return 1.10
	}
	return 1.15
}

// Leg is one market's part in a coin's price.
type Leg struct {
	Exchange string  `json:"exchange"`
	Quote    string  `json:"quote"`
	Price    float64 `json:"price"`  // in the quote currency
	USD      float64 `json:"usd"`    // in dollars
	Volume   float64 `json:"volume"` // 24 hours, in the coin
	AgeS     int64   `json:"ageS"`
	Weight   float64 `json:"weight"`        // its share of the price, 0..1
	Out      string  `json:"out,omitempty"` // why it was left out
}

// Blend is a coin's dollar price from its markets (quotes of any currency in conv: one unit of
// the quote currency in dollars), weighted as above, with outliers against prev (the coin's last
// price; 0 for none, when the markets' volume-weighted median stands in). It returns the index
// and each market's part, heaviest first.
func Blend(quotes []Quote, symbol string, conv map[string]float64, prev float64, now time.Time) (Index, []Leg, error) {
	ix := Index{Dropped: map[string]string{}, At: now}
	stable := Stable(symbol)
	var legs []Leg
	for _, q := range quotes {
		if q.Base != symbol || q.Price <= 0 {
			continue
		}
		if stable && q.QuoteCcy != "USD" && q.QuoteCcy != "USDT" {
			continue
		}
		fx, ok := conv[q.QuoteCcy]
		if !ok || fx <= 0 {
			continue // no price for the quote currency yet
		}
		age := now.Sub(q.At)
		if age < 0 {
			age = 0
		}
		legs = append(legs, Leg{Exchange: q.Exchange, Quote: q.QuoteCcy, Price: q.Price, USD: q.Price * fx,
			Volume: math.Max(q.Volume, 0), AgeS: int64(age.Seconds())})
	}
	if len(legs) == 0 {
		return ix, nil, errors.New("no market priced in a currency the box can convert")
	}
	anyVolume := false
	for _, l := range legs {
		if l.Volume > 0 {
			anyVolume = true
			break
		}
	}
	vol := func(l Leg) float64 {
		if !anyVolume {
			return 1
		}
		return l.Volume
	}
	// the reference the outlier rule measures against: the last price, else the weighted median
	ref := prev
	if ref <= 0 {
		ref = weightedMedian(legs, vol)
	}
	band := outlierBand(len(legs))
	weigh := func(ref float64) float64 {
		sum := 0.0
		for i := range legs {
			l := &legs[i]
			l.Out, l.Weight = "", 0
			if len(legs) > 2 && ref > 0 && (l.USD > band*ref || band*l.USD < ref) {
				l.Out = "off by " + strconv.FormatFloat(100*math.Abs(l.USD/ref-1), 'f', 1, 64) + "%"
				continue
			}
			l.Weight = vol(*l) * TimePenalty(time.Duration(l.AgeS)*time.Second)
			if l.Weight <= 0 {
				l.Out = "no volume"
			}
			sum += l.Weight
		}
		return sum
	}
	total := weigh(ref)
	if total <= 0 && prev > 0 {
		// every market moved past the band since the last price: measure against the markets
		total = weigh(weightedMedian(legs, vol))
	}
	if total <= 0 {
		return ix, legs, errors.New("every market was left out")
	}
	venues := map[string]bool{}
	paths := map[string]bool{}
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := range legs {
		l := &legs[i]
		if l.Weight <= 0 {
			ix.Dropped[l.Exchange+":"+l.Quote] = l.Out
			continue
		}
		l.Weight /= total
		ix.Price += l.Weight * l.USD
		ix.Markets++
		venues[l.Exchange] = true
		paths[l.Quote] = true
		lo, hi = math.Min(lo, l.USD), math.Max(hi, l.USD)
	}
	for v := range venues {
		ix.Used = append(ix.Used, v)
	}
	sort.Strings(ix.Used)
	for _, q := range ConvQuotes {
		if paths[q] {
			ix.Paths = append(ix.Paths, q)
		}
	}
	ix.N = len(ix.Used)
	if ix.Price > 0 {
		ix.Spread = (hi - lo) / ix.Price
	}
	sort.SliceStable(legs, func(i, j int) bool { return legs[i].Weight > legs[j].Weight })
	return ix, legs, nil
}

// weightedMedian is the dollar price half the weight sits either side of.
func weightedMedian(legs []Leg, vol func(Leg) float64) float64 {
	type pw struct{ p, w float64 }
	var xs []pw
	total := 0.0
	for _, l := range legs {
		w := vol(l) * TimePenalty(time.Duration(l.AgeS)*time.Second)
		if w <= 0 {
			w = 1e-9
		}
		xs = append(xs, pw{l.USD, w})
		total += w
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].p < xs[j].p })
	acc := 0.0
	for _, x := range xs {
		acc += x.w
		if acc >= total/2 {
			return x.p
		}
	}
	return xs[len(xs)-1].p
}

// ConvOrder is the order a minute's prices are worked out in: the conversion currencies first,
// each from the ones before it (USDT from USD markets, USDC from USD and USDT, BTC from the dollar
// and euro markets, ETH from those and BTC), then every other coin from all of them.
func ConvOrder(symbols []string) []string {
	first := []string{"USDT", "USDC", "BTC", "ETH"}
	seen := map[string]bool{}
	var out []string
	for _, s := range first {
		seen[s] = true
		out = append(out, s)
	}
	rest := append([]string(nil), symbols...)
	sort.Strings(rest)
	for _, s := range rest {
		s = strings.ToUpper(s)
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// BlendAll prices every symbol, in ConvOrder, each conversion currency's price
// joining conv as soon as it is made. eurUSD is the ECB's dollars per euro (0 when the box has no
// table yet: no EUR market then). prev is each coin's last price. It returns the prices and each
// coin's legs.
func BlendAll(quotes []Quote, symbols []string, eurUSD float64, prev map[string]float64, now time.Time) (map[string]Index, map[string][]Leg, map[string]string) {
	conv := map[string]float64{"USD": 1}
	if eurUSD > 0 {
		conv["EUR"] = eurUSD
	}
	have := map[string]bool{}
	for _, q := range quotes {
		have[q.Base] = true
	}
	out := map[string]Index{}
	legs := map[string][]Leg{}
	failed := map[string]string{}
	for _, sym := range ConvOrder(symbols) {
		if !have[sym] {
			continue
		}
		ix, ls, err := Blend(quotes, sym, conv, prev[sym], now)
		if err != nil {
			failed[sym] = err.Error()
			if IsConvQuote(sym) {
				conv[sym] = fallbackConv(sym)
			}
			continue
		}
		out[sym], legs[sym] = ix, ls
		if IsConvQuote(sym) {
			conv[sym] = ix.Price
		}
	}
	return out, legs, failed
}

// fallbackConv is a stablecoin's dollar when no market priced it (a dollar); 0 for the others,
// which then convert nothing.
func fallbackConv(sym string) float64 {
	if Stable(sym) {
		return 1
	}
	return 0
}
