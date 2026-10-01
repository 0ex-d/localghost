package main

// THE BOX READS THE MARKETS EVERY MINUTE. The tickers are the box's own, whatever the phone is on:
// a minute series cannot come from a phone Android wakes every quarter hour at best, so the seven
// venues are asked by the box itself once a minute (six all-pairs calls and Coinbase pair by
// pair, seventeen requests), and each minute's index per symbol and the market index's value go
// into the minute series. The ECB and the rank lists stay the phone's while it is on Wi-Fi (they
// move hourly or daily); on mobile data, or in silence, the box fetches those too, from where the
// marks say it left off. The days and the history are the box's own: the ECB's table back to 1999
// once, every quoted pair's daily candles once a day, the years of daily candles a page at a
// time, and the last thirty days of hourly and seven days of minute candles walked back a few
// pages a minute until they are whole. Everything goes through the one outbound client
// (internal/egress).

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/egress"
	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/rates"
	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

const (
	tickEvery     = time.Minute
	tickBudget    = 45 * time.Second // the history pages stop here, so a tick never runs into the next
	fetchGap      = 150 * time.Millisecond
	dailyPerTick  = 15 // daily candle refreshes a minute (once a day per pair)
	historyPages  = 12 // pages of hourly or minute history a minute, until whole
	housekeepings = 10 // minutes between the roll-ups and the market index rebuild
)

// fetchState is the loop's last decision, for the `rates` command and the health line.
type fetchState struct {
	mu       sync.Mutex
	at       time.Time
	proxy    bool
	why      string
	fetched  int
	backfill string
	pending  map[string]int
	took     time.Duration
}

func (f *fetchState) note(proxy bool, why string, fetched int, backfill string, took time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.at, f.proxy, f.why, f.fetched, f.backfill, f.took = time.Now(), proxy, why, fetched, backfill, took
}

func (f *fetchState) setPending(res string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil {
		f.pending = map[string]int{}
	}
	f.pending[res] = n
}

func (f *fetchState) snapshot() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]any{"phoneIsProxy": f.proxy, "why": f.why}
	if !f.at.IsZero() {
		out["lastTickAt"] = f.at.Unix()
		out["fetchedLastTick"] = f.fetched
		out["lastTickTook"] = f.took.Round(time.Millisecond).String()
	}
	if f.backfill != "" {
		out["backfill"] = f.backfill
	}
	if len(f.pending) > 0 {
		out["historyPagesPending"] = f.pending
	}
	return out
}

// ratesFetchLoop is the box's side of the rates. force makes one pass regardless of the phone.
func ratesFetchLoop(ctx context.Context, mount string, rs *ratesState, fs *fetchState, lg *slog.Logger, force <-chan struct{}) {
	client := egress.New()
	var db *poltergres.ReadWrite
	pass := func(forced bool) {
		if db == nil {
			cfg, err := hw.LoadServicesConfig(mount)
			if err != nil {
				return
			}
			db = poltergres.NewReadWrite(hw.SocketForMount(mount), cfg.Postgres.Port, cfg.Postgres.RWUser, cfg.Postgres.RWPass, cfg.Postgres.Name)
		}
		start := time.Now()
		now := start
		minute := now.Truncate(time.Minute)
		proxy, why := hw.PhoneNetFrom(db).Proxy(now)
		syms := tally.Symbols(db)
		marks := tally.Marks(db)
		var rows []egress.Fetched
		get := func(id, url string) bool {
			f, err := client.Get(ctx, id, url)
			if err != nil {
				return false // the context ended
			}
			rows = append(rows, f)
			time.Sleep(fetchGap)
			return true
		}
		// 1. the tickers, every minute, the box's own
		for _, src := range rates.TickerSources(syms) {
			if !get(src.ID, src.URL) {
				return
			}
		}
		// 2. the ECB and the rank lists: the phone's while it is on Wi-Fi
		if !proxy || forced {
			for _, src := range rates.PhoneSources() {
				if egress.Due(marks[src.ID], time.Duration(src.Every)*time.Minute, now) && !get(src.ID, src.URL) {
					return
				}
			}
		}
		// 3. the ECB's whole history, once (seven megabytes)
		if marks["ecb-hist"] == 0 {
			if f, err := client.GetCapped(ctx, "ecb-hist", rates.ECBHistoryURL, 16<<20); err == nil {
				rows = append(rows, f)
			}
		}
		// 4. the daily candles of every pair the venues were seen quoting, once a day each, then one
		// page of one market's years
		seen := tally.MarketsSeen(db, now)
		daily := 0
		for _, m := range seen {
			if daily >= dailyPerTick {
				break
			}
			id := "hist:" + m.ID()
			if !egress.Due(marks[id], 24*time.Hour, now) {
				continue
			}
			daily++
			if !get(id, m.CandlesURL(now.AddDate(0, 0, -8), now)) {
				return
			}
		}
		backfill := ""
		var bf *rates.Market
		var bfOldest string
		if m, from, to, bwhy := tally.NextBackfill(db, seen, now); m != nil {
			bf, bfOldest = m, tally.OldestDay(db, *m)
			backfill = bwhy
			if !get("hist:"+m.ID(), m.CandlesURL(from, to)) {
				return
			}
		} else {
			backfill = bwhy
		}
		batch := map[string]any{"fetchedAt": now.Unix(), "by": "box", "sources": rows}
		raw, _ := json.Marshal(batch)
		res, err := tally.IngestRates(db, raw, now)
		if err != nil {
			lg.Warn("rates fetched by the box, ingest failed", "fn", "ratesFetchLoop", "err", err)
			rs.note(err, tally.RatesResult{})
			db = nil
			return
		}
		rs.note(nil, res)
		if bf != nil && tally.OldestDay(db, *bf) == bfOldest {
			tally.MarkHistoryDone(db, *bf) // no older candle came: this venue's years end here
			backfill = bf.ID() + " is as far back as the venue goes"
		}
		// 5. the minute: each symbol's index and the market's value
		if mv, err := tally.RecordMinute(db, minute, res.Index); err != nil {
			lg.Warn("minute series not written", "fn", "ratesFetchLoop", "err", err)
		} else {
			lg.Debug("minute recorded", "fn", "ratesFetchLoop", "symbols", len(res.Index), "market", mv)
		}
		// 6. the history: the hours first (thirty days), then the minutes (a week), until the budget
		pages := tally.NextBarPages(db, tally.ResHour, syms, seen, now, historyPages)
		fs.setPending(tally.ResHour, len(pages))
		if left := historyPages - len(pages); left > 0 {
			mp := tally.NextBarPages(db, tally.ResMinute, syms, seen, now, left)
			fs.setPending(tally.ResMinute, len(mp))
			pages = append(pages, mp...)
		}
		walked := 0
		for _, p := range pages {
			if time.Since(start) > tickBudget {
				break
			}
			f, err := client.Get(ctx, p.ID(), p.URL())
			if err != nil {
				return
			}
			time.Sleep(fetchGap)
			if f.Status < 200 || f.Status > 299 || f.Body == "" {
				tally.BarFailed(db, p.Res, p.Market)
				continue
			}
			n, older, ierr := tally.IngestBars(db, p, []byte(f.Body))
			switch {
			case ierr != nil:
				tally.BarFailed(db, p.Res, p.Market)
			case !older:
				tally.MarkBarsDone(db, p.Res, p.Market) // nothing older came: this venue's window ends here
			}
			walked += n
		}
		if walked > 0 {
			backfill += " · history: " + itoa(walked) + " bars this minute"
		}
		// 7. the market index over the history, once the history for a resolution is whole
		for _, r := range []string{tally.ResHour, tally.ResMinute} {
			refoldWhenWhole(db, r, syms, seen, now, lg)
		}
		// 8. housekeeping: the hours from the minutes, the daily market index, what is kept
		if minute.Minute()%housekeepings == 0 || forced {
			if err := tally.RollupHours(db, now); err != nil {
				lg.Warn("hourly roll-up failed", "fn", "ratesFetchLoop", "err", err)
			}
			if n, err := tally.RebuildMarketIndex(db, now); err != nil {
				lg.Warn("market index rebuild failed", "fn", "ratesFetchLoop", "err", err)
			} else if n > 0 {
				lg.Debug("market index rebuilt", "fn", "ratesFetchLoop", "days", n)
			}
		}
		if minute.Minute() == 7 {
			tally.Prune(db, now)
		}
		fs.note(proxy, why, len(rows), backfill, time.Since(start))
		lg.Debug("rates minute", "fn", "ratesFetchLoop", "addresses", len(rows), "what", res.String(), "backfill", backfill, "took", time.Since(start).Round(time.Millisecond))
	}
	// on the minute, so the minute series' points sit on the minute
	wait := time.Until(time.Now().Truncate(time.Minute).Add(time.Minute + 2*time.Second))
	first := time.NewTimer(wait)
	defer first.Stop()
	var t *time.Ticker
	tc := func() <-chan time.Time {
		if t == nil {
			return nil
		}
		return t.C
	}
	defer func() {
		if t != nil {
			t.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			t = time.NewTicker(tickEvery)
			pass(false)
		case <-tc():
			pass(false)
		case <-force:
			pass(true)
		}
	}
}

// refoldWhenWhole makes the market index over a resolution's history once its walk is done (and
// again when a new symbol starts a new walk and finishes it).
func refoldWhenWhole(db *poltergres.ReadWrite, res string, syms []string, seen []rates.Market, now time.Time, lg *slog.Logger) {
	key := "series_folded_" + res
	pending := len(tally.NextBarPages(db, res, syms, seen, now, 1)) > 0
	rows, err := db.Query("SELECT value FROM settings WHERE key = $1", key)
	folded := err == nil && len(rows.Vals) == 1
	switch {
	case pending && folded:
		_ = db.Exec("DELETE FROM settings WHERE key = $1", key)
	case !pending && !folded && len(seen) > 0:
		ri := tally.Resolutions[res]
		n, err := tally.RefoldMarket(db, res, now.Add(-ri.Window), now)
		if err != nil {
			lg.Warn("market series refold failed", "fn", "refoldWhenWhole", "res", res, "err", err)
			return
		}
		_ = db.Exec("INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value", key, itoa(int(now.Unix())))
		lg.Info("market index made over the history", "fn", "refoldWhenWhole", "res", res, "points", n)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
