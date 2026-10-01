package tally

// THE SERIES: a price every minute for the last week and every hour for the last thirty days, per
// symbol followed and for the market index. Three ways a point is made, in the order they win:
//   live    , the minute's index from the venues' tickers, which the box reads every minute
//   minutes , an hour rolled up from the minutes it holds (open, high, low, close)
//   venues  , folded from the venues' own candles, fetched back when the box starts, for the
//             time before it was watching (FoldBar: the mean of the venues near their median)
// A live point is never overwritten by a folded one. The market index is made from the series the
// same way the daily one is made from the daily closes: the month's weights over the base prices,
// chained. Where it left off is the tables themselves, so a restart loses nothing.

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/rates"
)

// The two resolutions.
const (
	ResMinute = "1m"
	ResHour   = "1h"
)

// ResInfo is a resolution's step, how far back it is built, and how long it is kept.
type ResInfo struct {
	Step, Window, Keep time.Duration
}

// Resolutions is the two, by name.
var Resolutions = map[string]ResInfo{
	ResMinute: {time.Minute, 7 * 24 * time.Hour, 8 * 24 * time.Hour},
	ResHour:   {time.Hour, 30 * 24 * time.Hour, 35 * 24 * time.Hour},
}

// barPrefs is which venues a symbol's history is fetched from, best first; two are taken among
// those that quote it. Kraken (720 bars, whatever is asked) and Gemini (no paging) cannot walk a
// week of minutes back, so they are left to the hours.
var barPrefs = map[string][]string{
	ResHour:   {"binance", "coinbase", "kraken", "bitstamp", "bitfinex", "okx", "gemini"},
	ResMinute: {"binance", "bitstamp", "bitfinex", "coinbase", "okx"},
}

// usdtPrefs is where the USDT/USD leg's history comes from (it folds the USDT venues into dollars).
var usdtPrefs = map[string][]string{
	ResHour:   {"coinbase", "kraken", "bitstamp", "bitfinex"},
	ResMinute: {"bitstamp", "bitfinex", "coinbase"},
}

// execRows inserts many rows in statements of at most 400, the same insert for each.
func execRows(db *poltergres.ReadWrite, head, tail string, cols int, rows [][]any) error {
	for start := 0; start < len(rows); start += 400 {
		end := start + 400
		if end > len(rows) {
			end = len(rows)
		}
		var b strings.Builder
		b.WriteString(head)
		args := make([]any, 0, (end-start)*cols)
		for i, r := range rows[start:end] {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("(")
			for j := 0; j < cols; j++ {
				if j > 0 {
					b.WriteString(",")
				}
				fmt.Fprintf(&b, "$%d", len(args)+j+1)
			}
			b.WriteString(")")
			args = append(args, r...)
		}
		b.WriteString(tail)
		if err := db.Exec(b.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

// weightSet is a month's constituents and the chain value they start from.
type weightSet struct {
	cons  []rates.Constituent
	chain float64
}

// weightsFor is the weights a day is priced with: its own month's; for a month before the first
// one with weights, that first one's (the index as if it had existed, from 1000); for a month
// after the last (its first day, before a rebuild), the last one's.
func weightsFor(db *poltergres.ReadWrite, day string, cache map[string]weightSet) (weightSet, bool) {
	month := rates.MonthOf(day)
	if ws, ok := cache[month]; ok {
		return ws, len(ws.cons) > 0
	}
	use := month
	cons, _ := loadWeights(db, month)
	if len(cons) == 0 {
		if rows, err := db.Query("SELECT min(month) FROM crypto_market_weights WHERE month > $1", month); err == nil && len(rows.Vals) == 1 && rows.Vals[0][0] != nil {
			use = *rows.Vals[0][0]
		} else if rows, err := db.Query("SELECT max(month) FROM crypto_market_weights WHERE month < $1", month); err == nil && len(rows.Vals) == 1 && rows.Vals[0][0] != nil {
			use = *rows.Vals[0][0]
		}
		if use != month {
			cons, _ = loadWeights(db, use)
		}
	}
	ws := weightSet{cons: cons}
	if len(cons) > 0 {
		if use > month {
			ws.chain = rates.MarketIndexStart // before the first month: from the start value
		} else {
			ws.chain = chainBefore(db, use)
		}
	}
	cache[month] = ws
	return ws, len(cons) > 0
}

// lastCloses is the newest close per symbol before a time, within a span: the carried prices.
func lastCloses(db *poltergres.ReadWrite, res string, before int64, within time.Duration) map[string]float64 {
	out := map[string]float64{}
	rows, err := db.Query(`SELECT DISTINCT ON (symbol) symbol, close FROM crypto_series WHERE res = $1 AND ts < $2 AND ts >= $3 ORDER BY symbol, ts DESC`,
		res, before, before-int64(within.Seconds()))
	if err != nil {
		return out
	}
	for _, v := range rows.Vals {
		if len(v) == 2 && v[0] != nil && v[1] != nil {
			out[*v[0]], _ = strconv.ParseFloat(*v[1], 64)
		}
	}
	return out
}

// RecordMinute writes the minute's index per symbol into the minute series (source live) and the
// market's value for the minute. It returns the market value (0 when there are no weights yet).
func RecordMinute(db *poltergres.ReadWrite, minute time.Time, idx map[string]rates.Index) (float64, error) {
	ts := minute.Truncate(time.Minute).Unix()
	var rows [][]any
	prices := map[string]float64{}
	for sym, ix := range idx {
		if ix.Price <= 0 {
			continue
		}
		prices[sym] = ix.Price
		rows = append(rows, []any{ResMinute, ts, sym, ix.Price, ix.Price, ix.Price, ix.Price, ix.N, "live"})
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if err := execRows(db, "INSERT INTO crypto_series (res, ts, symbol, open, high, low, close, n, source) VALUES ",
		" ON CONFLICT (res, symbol, ts) DO UPDATE SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low, close = EXCLUDED.close, n = EXCLUDED.n, source = EXCLUDED.source",
		9, rows); err != nil {
		return 0, err
	}
	ws, ok := weightsFor(db, minute.UTC().Format("2006-01-02"), map[string]weightSet{})
	if !ok {
		return 0, nil
	}
	value, priced, _ := rates.Value(ws.chain, ws.cons, prices, lastCloses(db, ResMinute, ts, time.Hour))
	if value <= 0 {
		return 0, nil
	}
	return value, db.Exec(`INSERT INTO crypto_market_series (res, ts, open, high, low, close, priced, source) VALUES ($1,$2,$3,$3,$3,$3,$4,'live')
		ON CONFLICT (res, ts) DO UPDATE SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low, close = EXCLUDED.close, priced = EXCLUDED.priced, source = EXCLUDED.source`,
		ResMinute, ts, value, priced)
}

// RollupHours makes the hours from the minutes the box holds: every complete hour of the last
// eight days with at least thirty minutes, per symbol and for the market. An hour already rolled
// is rolled again when more minutes came (a backfill landing under it); a folded hour gives way.
func RollupHours(db *poltergres.ReadWrite, now time.Time) error {
	to := now.Truncate(time.Hour).Unix()
	from := now.Add(-Resolutions[ResMinute].Keep).Truncate(time.Hour).Unix()
	if err := db.Exec(`INSERT INTO crypto_series (res, ts, symbol, open, high, low, close, n, source)
		SELECT '1h', h, symbol, (array_agg(open ORDER BY ts))[1], max(high), min(low), (array_agg(close ORDER BY ts DESC))[1], count(*), 'minutes'
		FROM (SELECT ts - ts % 3600 AS h, ts, symbol, open, high, low, close FROM crypto_series WHERE res = '1m' AND ts >= $1 AND ts < $2) m
		GROUP BY h, symbol HAVING count(*) >= 30
		ON CONFLICT (res, symbol, ts) DO UPDATE SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low, close = EXCLUDED.close, n = EXCLUDED.n, source = EXCLUDED.source
		WHERE crypto_series.source <> 'minutes' OR crypto_series.n < EXCLUDED.n`, from, to); err != nil {
		return err
	}
	return db.Exec(`INSERT INTO crypto_market_series (res, ts, open, high, low, close, priced, source)
		SELECT '1h', h, (array_agg(close ORDER BY ts))[1], max(close), min(close), (array_agg(close ORDER BY ts DESC))[1], count(*), 'minutes'
		FROM (SELECT ts - ts % 3600 AS h, ts, close FROM crypto_market_series WHERE res = '1m' AND ts >= $1 AND ts < $2) m
		GROUP BY h HAVING count(*) >= 30
		ON CONFLICT (res, ts) DO UPDATE SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low, close = EXCLUDED.close, priced = EXCLUDED.priced, source = EXCLUDED.source
		WHERE crypto_market_series.source <> 'minutes' OR crypto_market_series.priced < EXCLUDED.priced`, from, to)
}

// Prune drops what each resolution keeps no longer.
func Prune(db *poltergres.ReadWrite, now time.Time) {
	for res, ri := range Resolutions {
		cut := now.Add(-ri.Keep).Unix()
		_ = db.Exec("DELETE FROM crypto_bars WHERE res = $1 AND ts < $2", res, cut)
		_ = db.Exec("DELETE FROM crypto_series WHERE res = $1 AND ts < $2", res, cut)
		_ = db.Exec("DELETE FROM crypto_market_series WHERE res = $1 AND ts < $2", res, cut)
	}
}

// BarPage is one page of one market's history to fetch.
type BarPage struct {
	Res      string
	Market   rates.Market
	From, To time.Time
	Oldest   int64 // the oldest bar held before the page (0 when none)
	Floor    int64 // the window's start: bars older than this are not kept
}

// URL is the page's address.
func (p BarPage) URL() string {
	return p.Market.BarsURL(Resolutions[p.Res].Step, p.From, p.To)
}

// ID is the page's source id in a batch.
func (p BarPage) ID() string { return "bars:" + p.Res + ":" + p.Market.ID() }

func barsDoneKey(res string, m rates.Market) string { return "bars_done_" + res + "_" + m.ID() }

// NextBarPages is the pages of history still to fetch for a resolution, at most max: for the USDT
// leg first, then each symbol in order, up to two venues among those seen quoting it (barPrefs),
// each walked back from the oldest bar held to the window's floor.
func NextBarPages(db *poltergres.ReadWrite, res string, symbols []string, seen []rates.Market, now time.Time, max int) []BarPage {
	ri, ok := Resolutions[res]
	if !ok || max <= 0 {
		return nil
	}
	done := barsDone(db, res)
	floor := now.Add(-ri.Window).Truncate(ri.Step)
	var out []BarPage
	for _, m := range barMarkets(res, symbols, seen) {
		if len(out) >= max {
			return out
		}
		if done[barsDoneKey(res, m)] {
			continue
		}
		oldest := int64(0)
		if rows, err := db.Query("SELECT coalesce(min(ts),0) FROM crypto_bars WHERE res = $1 AND exchange = $2 AND base = $3 AND quote = $4", res, m.Exchange, m.Base, m.Quote); err == nil && len(rows.Vals) == 1 && rows.Vals[0][0] != nil {
			oldest, _ = strconv.ParseInt(*rows.Vals[0][0], 10, 64)
		}
		var to time.Time
		switch {
		case oldest == 0:
			to = now.Truncate(ri.Step)
		case oldest <= floor.Unix():
			MarkBarsDone(db, res, m)
			continue
		default:
			to = time.Unix(oldest, 0).Add(-ri.Step)
		}
		from := to.Add(-time.Duration(m.PageBars()-1) * ri.Step)
		if from.Before(floor) {
			from = floor
		}
		out = append(out, BarPage{Res: res, Market: m, From: from, To: to, Oldest: oldest, Floor: floor.Unix()})
	}
	return out
}

// barsDone is the settings keys of the markets whose walk ended at a resolution.
func barsDone(db *poltergres.ReadWrite, res string) map[string]bool {
	done := map[string]bool{}
	if rows, err := db.Query("SELECT key FROM settings WHERE key LIKE $1", "bars_done_"+res+"_%"); err == nil {
		for _, v := range rows.Vals {
			if len(v) > 0 && v[0] != nil {
				done[*v[0]] = true
			}
		}
	}
	return done
}

// barMarkets is the markets whose history is walked back at a resolution, in the order they are
// walked: the USDT leg first, then each symbol in order, up to two venues among those seen
// quoting it (barPrefs).
func barMarkets(res string, symbols []string, seen []rates.Market) []rates.Market {
	bySym := map[string][]rates.Market{}
	for _, m := range seen {
		bySym[m.Base] = append(bySym[m.Base], m)
	}
	rank := func(prefs []string, ex string) int {
		for i, p := range prefs {
			if p == ex {
				return i
			}
		}
		return -1
	}
	choose := func(sym string) []rates.Market {
		prefs := barPrefs[res]
		if sym == "USDT" {
			prefs = usdtPrefs[res]
		}
		var cands []rates.Market
		for _, m := range bySym[sym] {
			if sym == "USDT" && m.Quote != "USD" {
				continue
			}
			if rank(prefs, m.Exchange) >= 0 {
				cands = append(cands, m)
			}
		}
		sort.Slice(cands, func(i, j int) bool {
			ri, rj := rank(prefs, cands[i].Exchange), rank(prefs, cands[j].Exchange)
			if ri != rj {
				return ri < rj
			}
			return cands[i].Quote < cands[j].Quote
		})
		if len(cands) > 2 {
			cands = cands[:2]
		}
		return cands
	}
	var out []rates.Market
	seenSym := map[string]bool{}
	for _, sym := range append([]string{"USDT"}, symbols...) {
		if seenSym[sym] {
			continue
		}
		seenSym[sym] = true
		out = append(out, choose(sym)...)
	}
	return out
}

// MarkBarsDone records that a venue has nothing older for a market at a resolution (or that it
// kept failing).
func MarkBarsDone(db *poltergres.ReadWrite, res string, m rates.Market) {
	_ = db.Exec("INSERT INTO settings (key, value) VALUES ($1, '1') ON CONFLICT (key) DO NOTHING", barsDoneKey(res, m))
}

// BarFailed counts a failed page; the third failure in a row ends that market's walk.
func BarFailed(db *poltergres.ReadWrite, res string, m rates.Market) {
	key := "bars_fail_" + res + "_" + m.ID()
	n := 0
	if rows, err := db.Query("SELECT value FROM settings WHERE key = $1", key); err == nil && len(rows.Vals) == 1 && rows.Vals[0][0] != nil {
		n, _ = strconv.Atoi(*rows.Vals[0][0])
	}
	n++
	_ = db.Exec("INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value", key, strconv.Itoa(n))
	if n >= 3 {
		MarkBarsDone(db, res, m)
	}
}

// IngestBars stores one page of a market's bars and folds the symbol's series over the page's
// span. It says whether anything older than what was held came (the walk ends when not).
func IngestBars(db *poltergres.ReadWrite, page BarPage, body []byte) (stored int, older bool, err error) {
	bars, perr := rates.ParseBars(page.Market, body)
	if perr != nil {
		return 0, false, perr
	}
	ri := Resolutions[page.Res]
	var rows [][]any
	minTS, maxTS := int64(0), int64(0)
	for _, b := range bars {
		ts := b.TS - b.TS%int64(ri.Step.Seconds())
		if ts < page.Floor {
			continue // a venue that answers more than was asked (Gemini, Kraken): only the window is kept
		}
		rows = append(rows, []any{page.Res, ts, page.Market.Exchange, page.Market.Base, page.Market.Quote, b.Open, b.High, b.Low, b.Close, b.Volume})
		if minTS == 0 || ts < minTS {
			minTS = ts
		}
		if ts > maxTS {
			maxTS = ts
		}
	}
	if len(rows) == 0 {
		return 0, false, nil
	}
	if err := execRows(db, "INSERT INTO crypto_bars (res, ts, exchange, base, quote, open, high, low, close, volume) VALUES ",
		" ON CONFLICT (res, exchange, base, quote, ts) DO UPDATE SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low, close = EXCLUDED.close, volume = EXCLUDED.volume",
		10, rows); err != nil {
		return 0, false, err
	}
	older = page.Oldest == 0 || minTS < page.Oldest
	return len(rows), older, FoldSeries(db, page.Res, page.Market.Base, minTS, maxTS)
}

// FoldSeries makes a symbol's series points from the venues' bars over a span, where the box has
// no live or rolled point of its own.
func FoldSeries(db *poltergres.ReadWrite, res, symbol string, from, to int64) error {
	rows, err := db.Query(`SELECT ts, exchange, quote, open, high, low, close, volume FROM crypto_bars WHERE res = $1 AND base = $2 AND ts >= $3 AND ts <= $4`, res, symbol, from, to)
	if err != nil {
		return err
	}
	byTS := map[int64]map[rates.Market]rates.Bar{}
	for _, v := range rows.Vals {
		if len(v) < 8 || v[0] == nil || v[1] == nil || v[2] == nil {
			continue
		}
		ts, _ := strconv.ParseInt(*v[0], 10, 64)
		f := func(i int) float64 { x, _ := strconv.ParseFloat(deref(v[i]), 64); return x }
		if byTS[ts] == nil {
			byTS[ts] = map[rates.Market]rates.Bar{}
		}
		byTS[ts][rates.Market{Exchange: *v[1], Base: symbol, Quote: *v[2]}] = rates.Bar{TS: ts, Open: f(3), High: f(4), Low: f(5), Close: f(6), Volume: f(7)}
	}
	usdt := map[int64]float64{}
	if symbol != "USDT" {
		if rows, err := db.Query(`SELECT ts, close FROM crypto_bars WHERE res = $1 AND base = 'USDT' AND quote = 'USD' AND ts >= $2 AND ts <= $3`, res, from, to); err == nil {
			legs := map[int64][]float64{}
			for _, v := range rows.Vals {
				if len(v) == 2 && v[0] != nil && v[1] != nil {
					ts, _ := strconv.ParseInt(*v[0], 10, 64)
					c, _ := strconv.ParseFloat(*v[1], 64)
					legs[ts] = append(legs[ts], c)
				}
			}
			for ts, cs := range legs {
				usdt[ts], _ = filteredMeanOf(cs)
			}
		}
	}
	var out [][]any
	for ts, bars := range byTS {
		b, n := rates.FoldBar(bars, symbol, usdt[ts])
		if n == 0 {
			continue
		}
		out = append(out, []any{res, ts, symbol, b.Open, b.High, b.Low, b.Close, n, "venues"})
	}
	return execRows(db, "INSERT INTO crypto_series (res, ts, symbol, open, high, low, close, n, source) VALUES ",
		" ON CONFLICT (res, symbol, ts) DO UPDATE SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low, close = EXCLUDED.close, n = EXCLUDED.n, source = EXCLUDED.source WHERE crypto_series.source = 'venues'",
		9, out)
}

func filteredMeanOf(vals []float64) (float64, int) {
	bars := map[rates.Market]rates.Bar{}
	for i, v := range vals {
		bars[rates.Market{Exchange: strconv.Itoa(i), Base: "X", Quote: "USD"}] = rates.Bar{Open: v, High: v, Low: v, Close: v}
	}
	b, n := rates.FoldBar(bars, "X", 1)
	return b.Close, n
}

// RefoldMarket makes the market index's points over a span from the series, where it has no live
// or rolled point: each step the month's weights over the constituents' closes, a constituent with
// no close carried from the step before. A day at a time, so a week of minutes stays small.
func RefoldMarket(db *poltergres.ReadWrite, res string, from, to time.Time) (int, error) {
	ri, ok := Resolutions[res]
	if !ok {
		return 0, errors.New("no such resolution " + res)
	}
	step := int64(ri.Step.Seconds())
	start := from.Truncate(ri.Step).Unix()
	end := to.Truncate(ri.Step).Unix()
	cache := map[string]weightSet{}
	carried := lastCloses(db, res, start, 6*time.Hour)
	written := 0
	for dayStart := start; dayStart <= end; dayStart += 86400 {
		dayEnd := dayStart + 86400 - step
		if dayEnd > end {
			dayEnd = end
		}
		rows, err := db.Query("SELECT ts, symbol, close FROM crypto_series WHERE res = $1 AND ts >= $2 AND ts <= $3", res, dayStart, dayEnd)
		if err != nil {
			return written, err
		}
		at := map[int64]map[string]float64{}
		for _, v := range rows.Vals {
			if len(v) == 3 && v[0] != nil && v[1] != nil && v[2] != nil {
				ts, _ := strconv.ParseInt(*v[0], 10, 64)
				c, _ := strconv.ParseFloat(*v[2], 64)
				if at[ts] == nil {
					at[ts] = map[string]float64{}
				}
				at[ts][*v[1]] = c
			}
		}
		var out [][]any
		for ts := dayStart; ts <= dayEnd; ts += step {
			prices := at[ts]
			if len(prices) == 0 {
				continue
			}
			ws, ok := weightsFor(db, time.Unix(ts, 0).UTC().Format("2006-01-02"), cache)
			if !ok {
				continue
			}
			value, priced, _ := rates.Value(ws.chain, ws.cons, prices, carried)
			for s, p := range prices {
				carried[s] = p
			}
			if value <= 0 || priced == 0 {
				continue
			}
			out = append(out, []any{res, ts, value, value, value, value, priced, "venues"})
		}
		if err := execRows(db, "INSERT INTO crypto_market_series (res, ts, open, high, low, close, priced, source) VALUES ",
			" ON CONFLICT (res, ts) DO UPDATE SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low, close = EXCLUDED.close, priced = EXCLUDED.priced, source = EXCLUDED.source WHERE crypto_market_series.source = 'venues'",
			8, out); err != nil {
			return written, err
		}
		written += len(out)
	}
	return written, nil
}

// SeriesPoint is one point of a series.
type SeriesPoint struct {
	TS     int64   `json:"ts"`
	Open   float64 `json:"o"`
	High   float64 `json:"h"`
	Low    float64 `json:"l"`
	Close  float64 `json:"c"`
	N      int     `json:"n"`
	Source string  `json:"src"`
}

// Series reads a symbol's (or the market index's) points between two times, oldest first.
func Series(db *poltergres.ReadWrite, code, res string, from, to time.Time) ([]SeriesPoint, error) {
	if _, ok := Resolutions[res]; !ok {
		return nil, errors.New("res is 1m or 1h")
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	var rows *poltergres.Rows
	var err error
	if code == rates.MarketIndexCode {
		rows, err = db.Query("SELECT ts, open, high, low, close, priced, source FROM crypto_market_series WHERE res = $1 AND ts >= $2 AND ts <= $3 ORDER BY ts", res, from.Unix(), to.Unix())
	} else {
		rows, err = db.Query("SELECT ts, open, high, low, close, n, source FROM crypto_series WHERE res = $1 AND symbol = $2 AND ts >= $3 AND ts <= $4 ORDER BY ts", res, code, from.Unix(), to.Unix())
	}
	if err != nil {
		return nil, err
	}
	out := make([]SeriesPoint, 0, len(rows.Vals))
	for _, v := range rows.Vals {
		if len(v) < 7 || v[0] == nil {
			continue
		}
		var p SeriesPoint
		p.TS, _ = strconv.ParseInt(*v[0], 10, 64)
		p.Open, _ = strconv.ParseFloat(deref(v[1]), 64)
		p.High, _ = strconv.ParseFloat(deref(v[2]), 64)
		p.Low, _ = strconv.ParseFloat(deref(v[3]), 64)
		p.Close, _ = strconv.ParseFloat(deref(v[4]), 64)
		p.N, _ = strconv.Atoi(deref(v[5]))
		p.Source = deref(v[6])
		out = append(out, p)
	}
	return out, nil
}

// SeriesDepth is how far each resolution goes back and how full it is, for the drill-in.
type SeriesDepth struct {
	Res     string `json:"res"`
	Oldest  int64  `json:"oldest"`
	Points  int    `json:"points"`  // the market index's points
	Symbols int    `json:"symbols"` // symbols with a point in the newest step
	Pending int    `json:"pending"` // markets whose history is still being walked back
}

// Depth reads the series' depth per resolution.
func Depth(db *poltergres.ReadWrite, symbols []string, now time.Time) []SeriesDepth {
	var out []SeriesDepth
	for _, res := range []string{ResMinute, ResHour} {
		d := SeriesDepth{Res: res}
		if rows, err := db.Query("SELECT coalesce(min(ts),0), count(*) FROM crypto_market_series WHERE res = $1", res); err == nil && len(rows.Vals) == 1 {
			d.Oldest, _ = strconv.ParseInt(deref(rows.Vals[0][0]), 10, 64)
			d.Points, _ = strconv.Atoi(deref(rows.Vals[0][1]))
		}
		if rows, err := db.Query("SELECT count(DISTINCT symbol) FROM crypto_series WHERE res = $1 AND ts >= $2", res, now.Add(-2*Resolutions[res].Step).Unix()); err == nil && len(rows.Vals) == 1 {
			d.Symbols, _ = strconv.Atoi(deref(rows.Vals[0][0]))
		}
		d.Pending = len(NextBarPages(db, res, symbols, MarketsSeen(db, now), now, 1000))
		out = append(out, d)
	}
	return out
}
