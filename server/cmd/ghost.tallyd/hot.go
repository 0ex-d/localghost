package main

// THE PHONE'S COPY, AT ONCE. After every minute (and every batch the phone sends) tallyd writes
// /v1/rates as it stands to the vault's Redis (hw.HotRates), so secd answers from memory instead of
// four queries. And the fast lane: BTC, ETH and SOL every five seconds from every venue that
// weighs in the index (twelve small requests across six venues, rates.FastAsks, each venue's
// share well inside its public limit), each coin's price made the minute's way and kept in Redis
// only (hw.HotFast); the minute series stays the record. A minute of the lane is one fetch-log
// line per venue.

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/apparedis"
	"github.com/LocalGhostDao/localghost/server/internal/egress"
	"github.com/LocalGhostDao/localghost/server/internal/feedstat"
	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/rates"
)

// hotRedis is tallyd's one Redis line, made on first use.
type hotRedis struct {
	mu    sync.Mutex
	mount string
	rd    *apparedis.ReadWrite
	pg    *poltergres.ReadWrite // the lane's own line, for its fetch-log line
}

func (h *hotRedis) db() *poltergres.ReadWrite {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pg == nil {
		if cfg, err := hw.LoadServicesConfig(h.mount); err == nil {
			h.pg = poltergres.NewReadWrite(hw.SocketForMount(h.mount), cfg.Postgres.Port, cfg.Postgres.RWUser, cfg.Postgres.RWPass, cfg.Postgres.Name)
		}
	}
	return h.pg
}

func (h *hotRedis) get() *apparedis.ReadWrite {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rd == nil {
		if rd, err := hw.HotRedis(h.mount); err == nil {
			h.rd = rd
		}
	}
	return h.rd
}

// putRates writes /v1/rates to Redis as Postgres now has it.
func (h *hotRedis) putRates(db *poltergres.ReadWrite, now time.Time, lg *slog.Logger) {
	rd := h.get()
	if rd == nil || db == nil {
		return
	}
	d, err := hw.RatesDocNow(db, now)
	if err != nil {
		return
	}
	if err := hw.HotPut(rd, hw.HotRates, d, hw.HotRatesTTL); err != nil {
		lg.Debug("rates not put in redis", "fn", "putRates", "err", err)
	}
}

// fastState is the lane's last minute per venue, for the `rates` command.
type fastState struct {
	mu     sync.Mutex
	venues map[string]venueMinute // the last full minute
	lastOK time.Time
	prices map[string]hw.FastPrice
}

// venueMinute is one venue's minute of the fast lane.
type venueMinute struct {
	Asked    int    `json:"asked"`
	Answered int    `json:"answered"`
	TookMs   []int  `json:"-"`
	Median   int    `json:"typicalMs"`
	Err      string `json:"lastError,omitempty"`
}

func (f *fastState) snapshot() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]any{"every": hw.FastEvery.String(), "symbols": hw.FastSymbols, "venues": f.venues}
	if !f.lastOK.IsZero() {
		out["lastAt"] = f.lastOK.Unix()
	}
	if len(f.prices) > 0 {
		out["prices"] = f.prices
	}
	return out
}

// fastLoop asks every venue that weighs in the index for the fast coins every five seconds, the
// venues side by side (each venue's calls one after the other), makes each coin's index the
// minute's way (rates.Blend) and puts it in Redis. A coin no venue answered for keeps its last price until it
// is too old to use (hw.FastFresh). A minute of the lane is one fetch-log line per venue.
func fastLoop(ctx context.Context, h *hotRedis, fs *fastState, lg *slog.Logger) {
	client := egress.NewKeepAlive(2)
	asks := rates.FastAsks(hw.FastSymbols)
	byVenue := map[string][]rates.FastAsk{}
	for _, a := range asks {
		byVenue[a.Venue] = append(byVenue[a.Venue], a)
	}
	want := map[string]bool{}
	for _, s := range hw.FastSymbols {
		want[s] = true
	}
	t := time.NewTicker(hw.FastEvery)
	defer t.Stop()
	last := hw.Fast{Prices: map[string]hw.FastPrice{}}
	minute := time.Now().Truncate(time.Minute)
	tally := map[string]*venueMinute{}
	flush := func(now time.Time) {
		if len(tally) == 0 {
			return
		}
		var logged []feedstat.Entry
		done := map[string]venueMinute{}
		for v, m := range tally {
			sort.Ints(m.TookMs)
			if len(m.TookMs) > 0 {
				m.Median = m.TookMs[len(m.TookMs)/2]
			}
			logged = append(logged, feedstat.Entry{Source: "fast:" + v, Kind: feedstat.KindFast, By: "box",
				OK: m.Answered == m.Asked, Items: m.Answered, TookMs: m.Median, Error: m.Err})
			done[v] = *m
		}
		if db := h.db(); db != nil {
			_ = feedstat.Log(db, now, logged)
		}
		fs.mu.Lock()
		fs.venues = done
		fs.mu.Unlock()
		tally = map[string]*venueMinute{}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if m := now.Truncate(time.Minute); m.After(minute) {
				flush(now)
				minute = m
			}
			rd := h.get()
			if rd == nil {
				continue
			}
			var mu sync.Mutex
			var quotes []rates.Quote
			var wg sync.WaitGroup
			for venue, list := range byVenue {
				wg.Add(1)
				go func(venue string, list []rates.FastAsk) {
					defer wg.Done()
					for _, a := range list {
						f, err := client.Get(ctx, a.ID, a.URL)
						if err != nil {
							return // the context ended
						}
						var qs []rates.Quote
						why := ""
						if f.Error != "" || f.Status < 200 || f.Status > 299 {
							why = fetchWhy(f)
						} else if qs, err = rates.ParseFast(a, []byte(f.Body), want, now); err != nil {
							why = "not a ticker: " + err.Error()
						}
						mu.Lock()
						m := tally[venue]
						if m == nil {
							m = &venueMinute{}
							tally[venue] = m
						}
						m.Asked++
						m.TookMs = append(m.TookMs, f.TookMs)
						if why == "" {
							m.Answered++
							quotes = append(quotes, qs...)
						} else {
							m.Err = why
						}
						mu.Unlock()
					}
				}(venue, list)
			}
			wg.Wait()
			if ctx.Err() != nil {
				return
			}
			// USDT's dollar price from the minute (it moves in the fourth decimal)
			usdt := 1.0
			var doc hw.RatesDoc
			if hw.HotGet(rd, hw.HotRates, &doc) {
				if u, ok := doc.Index["USDT"]; ok && u.Price > 0.9 && u.Price < 1.1 {
					usdt = u.Price
				}
			}
			made := 0
			stamp := time.Now()
			for _, sym := range hw.FastSymbols {
				prev := last.Prices[sym].Price
				if prev <= 0 {
					prev = doc.Index[sym].Price
				}
				ix, err := rates.FastIndex(quotes, sym, usdt, prev, stamp)
				if err != nil {
					continue
				}
				last.Prices[sym] = hw.FastPrice{Price: ix.Price, At: stamp.UnixMilli(), N: ix.N, Used: ix.Used, Spread: ix.Spread, Markets: ix.Markets, Paths: ix.Paths}
				made++
			}
			if made == 0 {
				continue
			}
			last.At = stamp.UnixMilli()
			if err := hw.HotPut(rd, hw.HotFast, last, hw.HotFastTTL); err != nil {
				lg.Debug("fast prices not put in redis", "fn", "fastLoop", "err", err)
				continue
			}
			fs.mu.Lock()
			fs.lastOK = stamp
			fs.prices = map[string]hw.FastPrice{}
			for k, v := range last.Prices {
				fs.prices[k] = v
			}
			fs.mu.Unlock()
		}
	}
}

func fetchWhy(f egress.Fetched) string {
	if f.Error != "" {
		return f.Error
	}
	return "HTTP " + itoa(f.Status)
}
