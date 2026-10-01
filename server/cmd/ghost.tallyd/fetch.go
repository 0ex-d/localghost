package main

// THE BOX FETCHES ITS OWN RATES when the phone cannot: the phone says every quarter hour what
// network it is on, and while it is on Wi-Fi it fetches the tickers and hands them over (the box
// stays off the wire for those); on mobile data, or in silence, this loop fetches what is due
// from where the marks say it left off, through the one outbound client (internal/egress). The
// days are the box's own whatever the phone is on: the ECB's table back to 1999 once, every
// quoted pair's daily candles once a day, and a page a tick walking the history back through
// the years until a venue has nothing older. After every batch the market index is brought up
// to date.

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
	fetchTick    = 5 * time.Minute
	fetchPerTick = 48 // addresses a tick at most; the rest wait for the next
	fetchGap     = 250 * time.Millisecond
)

// fetchState is the loop's last decision, for the `rates` command and the health line.
type fetchState struct {
	mu       sync.Mutex
	at       time.Time
	proxy    bool
	why      string
	fetched  int
	backfill string
}

func (f *fetchState) note(proxy bool, why string, fetched int, backfill string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.at, f.proxy, f.why, f.fetched, f.backfill = time.Now(), proxy, why, fetched, backfill
}

func (f *fetchState) snapshot() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]any{"phoneIsProxy": f.proxy, "why": f.why}
	if !f.at.IsZero() {
		out["lastDecisionAt"] = f.at.Unix()
		out["fetchedLastTick"] = f.fetched
	}
	if f.backfill != "" {
		out["backfill"] = f.backfill
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
		now := time.Now()
		proxy, why := hw.PhoneNetFrom(db).Proxy(now)
		syms := tally.Symbols(db)
		marks := tally.Marks(db)
		var rows []egress.Fetched
		// the tickers, the ECB and the rank lists: the phone's while it is on Wi-Fi
		if !proxy || forced {
			for _, src := range rates.Sources(syms, now) {
				if len(rows) >= fetchPerTick {
					break
				}
				if !egress.Due(marks[src.ID], time.Duration(src.Every)*time.Minute, now) {
					continue
				}
				f, err := client.Get(ctx, src.ID, src.URL)
				if err != nil {
					return // the context ended
				}
				rows = append(rows, f)
				time.Sleep(fetchGap)
			}
		}
		// the ECB's whole history, once (seven megabytes)
		if marks["ecb-hist"] == 0 {
			if f, err := client.GetCapped(ctx, "ecb-hist", rates.ECBHistoryURL, 16<<20); err == nil {
				rows = append(rows, f)
			}
		}
		// the daily candles of every pair the venues were seen quoting, once a day each (the box's
		// own, whatever the phone is on), then one page of one market's history
		seen := tally.MarketsSeen(db, now)
		for _, m := range seen {
			if len(rows) >= fetchPerTick {
				break
			}
			id := "hist:" + m.ID()
			if !egress.Due(marks[id], 24*time.Hour, now) {
				continue
			}
			f, err := client.Get(ctx, id, m.CandlesURL(now.AddDate(0, 0, -8), now))
			if err != nil {
				return
			}
			rows = append(rows, f)
			time.Sleep(fetchGap)
		}
		backfill := ""
		var bf *rates.Market
		var bfOldest string
		if m, from, to, bwhy := tally.NextBackfill(db, seen, now); m != nil {
			bf, bfOldest = m, tally.OldestDay(db, *m)
			backfill = bwhy
			if f, err := client.Get(ctx, "hist:"+m.ID(), m.CandlesURL(from, to)); err == nil {
				rows = append(rows, f)
			}
		} else {
			backfill = bwhy
		}
		if len(rows) == 0 {
			fs.note(proxy, why+"; nothing due", 0, backfill)
			// the market index still wants its daily value (the phone's batches may have landed)
			if n, err := tally.RebuildMarketIndex(db, now); err != nil {
				lg.Warn("market index rebuild failed", "fn", "ratesFetchLoop", "err", err)
			} else if n > 0 {
				lg.Debug("market index rebuilt", "fn", "ratesFetchLoop", "days", n)
			}
			return
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
		if bf != nil {
			// no older candle came: this venue's history ends here
			if tally.OldestDay(db, *bf) == bfOldest {
				tally.MarkHistoryDone(db, *bf)
				backfill = bf.ID() + " is as far back as the venue goes"
			}
		}
		fs.note(proxy, why, len(rows), backfill)
		lg.Info("rates fetched by the box", "fn", "ratesFetchLoop", "why", why, "addresses", len(rows), "what", res.String(), "backfill", backfill)
		if n, err := tally.RebuildMarketIndex(db, now); err != nil {
			lg.Warn("market index rebuild failed", "fn", "ratesFetchLoop", "err", err)
		} else if n > 0 {
			lg.Info("market index rebuilt", "fn", "ratesFetchLoop", "days", n)
		}
	}
	t := time.NewTicker(fetchTick)
	defer t.Stop()
	// a first look a minute in, so a box that starts with the phone away has numbers soon
	first := time.NewTimer(time.Minute)
	defer first.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			pass(false)
		case <-t.C:
			pass(false)
		case <-force:
			pass(true)
		}
	}
}
