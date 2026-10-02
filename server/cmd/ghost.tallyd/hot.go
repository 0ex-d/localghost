package main

// THE PHONE'S COPY, AT ONCE. After every minute (and every batch the phone sends) tallyd writes
// /v1/rates as it stands to the vault's Redis (hw.HotRates), so secd answers from memory instead of
// four queries. And the fast lane: BTC, ETH and SOL from Coinbase's ticker every five seconds
// (three small requests, well inside its public limit), kept in Redis only (hw.HotFast); the
// minute series stays the record. A minute of the lane goes in the fetch log as one line.

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

// fastState is the lane's last minute, for the `rates` command.
type fastState struct {
	mu     sync.Mutex
	at     time.Time
	asked  int
	ok     int
	lastOK time.Time
	err    string
}

func (f *fastState) snapshot() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]any{"every": hw.FastEvery.String(), "symbols": hw.FastSymbols, "askedThisMinute": f.asked, "answeredThisMinute": f.ok}
	if !f.lastOK.IsZero() {
		out["lastAnswerAt"] = f.lastOK.Unix()
	}
	if f.err != "" {
		out["lastError"] = f.err
	}
	return out
}

// fastLoop asks Coinbase for the fast coins every five seconds and puts their last trades in
// Redis. A coin Coinbase does not answer keeps its last price until it is too old to use.
func fastLoop(ctx context.Context, h *hotRedis, fs *fastState, lg *slog.Logger) {
	client := egress.New()
	t := time.NewTicker(hw.FastEvery)
	defer t.Stop()
	last := hw.Fast{Prices: map[string]hw.FastPrice{}}
	minute := time.Now().Truncate(time.Minute)
	var took []int
	asked, ok, errText := 0, 0, ""
	flush := func(now time.Time) {
		if asked == 0 {
			return
		}
		sort.Ints(took)
		e := feedstat.Entry{Source: "coinbase:fast", Kind: feedstat.KindFast, By: "box", OK: ok == asked, Items: ok, Error: errText}
		if len(took) > 0 {
			e.TookMs = took[len(took)/2]
		}
		if db := h.db(); db != nil {
			_ = feedstat.Log(db, now, []feedstat.Entry{e})
		}
		fs.mu.Lock()
		fs.at, fs.asked, fs.ok, fs.err = now, asked, ok, errText
		fs.mu.Unlock()
		asked, ok, errText, took = 0, 0, "", took[:0]
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
			got := 0
			for _, sym := range hw.FastSymbols {
				m := rates.Market{Exchange: "coinbase", Base: sym, Quote: "USD"}
				asked++
				f, err := client.Get(ctx, "coinbase:fast:"+sym, m.TickerURL())
				if err != nil {
					return // the context ended
				}
				took = append(took, f.TookMs)
				if f.Error != "" || f.Status < 200 || f.Status > 299 {
					errText = sym + ": " + fetchWhy(f)
					continue
				}
				q, perr := rates.ParseTicker(m, []byte(f.Body), now)
				if perr != nil || q.Price <= 0 {
					errText = sym + ": not a ticker"
					continue
				}
				ok++
				got++
				last.Prices[sym] = hw.FastPrice{Price: q.Price, At: q.At.UnixMilli()}
			}
			if got == 0 {
				continue
			}
			last.At = time.Now().UnixMilli()
			if err := hw.HotPut(rd, hw.HotFast, last, hw.HotFastTTL); err != nil {
				lg.Debug("fast prices not put in redis", "fn", "fastLoop", "err", err)
				continue
			}
			fs.mu.Lock()
			fs.lastOK = now
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
