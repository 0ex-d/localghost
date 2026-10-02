package hw

// WHAT THE PHONE WANTS AT ONCE sits in the vault's Redis (apparedis): the prices, the rank list and
// the market index as /v1/rates answers them (rewritten by tallyd every minute), BTC, ETH and SOL
// every five seconds (tallyd's fast lane: every venue that weighs in the index, rates.FastAsks,
// each coin's price made the minute's way, rates.MakeIndex), and the last two days of news with
// the brief as /v1/news answers them (rewritten by synthd whenever a story, summary or brief
// changes). Postgres stays the record; Redis is the copy that answers in a millisecond, gone on a
// restart and rebuilt from Postgres on the first read that misses.

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/apparedis"
)

const (
	HotRates = "hot:rates"  // the /v1/rates doc
	HotFast  = "hot:fast"   // BTC, ETH, SOL every five seconds, from every venue
	HotNews  = "hot:news"   // the /v1/news doc, two days
	HotSpark = "hot:sparks" // a week of hourly closes per coin, for CRYPTO's rows

	HotRatesTTL = 5 * time.Minute // tallyd rewrites it every minute
	HotFastTTL  = time.Minute     // a fast price a minute old is not fast
	HotNewsTTL  = 30 * time.Minute
	HotSparkTTL = 10 * time.Minute
	FastEvery   = 5 * time.Second
	FastFresh   = 30 * time.Second // older than this, the minute's index stands
	NewsHotDays = 2
)

// FastSymbols are the coins asked every five seconds; the rest move with the minute.
var FastSymbols = []string{"BTC", "ETH", "SOL"}

// FastPrice is one coin's index from the fast lane: the venues' last trades, volume-weighted.
type FastPrice struct {
	Price   float64  `json:"price"`
	At      int64    `json:"at"` // unix ms, when the lane made it
	N       int      `json:"n"`  // venues that went in
	Used    []string `json:"used"`
	Spread  float64  `json:"spread"`
	Markets int      `json:"markets"`
	Paths   []string `json:"paths"`
}

// Fast is the fast lane's last pass.
type Fast struct {
	At     int64                `json:"at"` // unix ms, when the pass finished
	Prices map[string]FastPrice `json:"prices"`
}

// HotPut writes v as JSON under key for ttl.
func HotPut(rd *apparedis.ReadWrite, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = rd.Do("SET", key, string(b), "EX", strconv.Itoa(int(ttl.Seconds())))
	return err
}

// HotGet reads key's JSON into v; false when absent or unreadable.
func HotGet(rd *apparedis.ReadWrite, key string, v any) bool {
	s, ok, err := rd.Get(key)
	if err != nil || !ok || s == "" {
		return false
	}
	return json.Unmarshal([]byte(s), v) == nil
}

// HotRedis is the vault's Redis as a daemon reaches it (tallyd, synthd), from the mount's config.
func HotRedis(mount string) (*apparedis.ReadWrite, error) {
	cfg, err := LoadServicesConfig(mount)
	if err != nil {
		return nil, err
	}
	return apparedis.NewReadWrite(cfg.Redis.Port, cfg.Redis.RWUser, cfg.Redis.RWPass), nil
}

// ApplyFast puts the fast lane's prices over the minute's index: a coin's price and time become
// the trade's, its 24-hour change is moved to match (the same 24-hour-ago price), and the flat BTC
// numbers follow, with the venues that went in. A price older than FastFresh, or more than 5% off
// the minute's index, is left out. Returns how many coins it moved.
func ApplyFast(s *RatesSnapshot, f Fast, now time.Time) int {
	n := 0
	for sym, p := range f.Prices {
		if p.Price <= 0 || now.Sub(time.UnixMilli(p.At)) > FastFresh {
			continue
		}
		r, ok := s.Index[sym]
		if !ok || r.Price <= 0 {
			continue
		}
		if d := p.Price/r.Price - 1; d > 0.05 || d < -0.05 {
			continue
		}
		if at := p.At / 1000; at < r.At {
			continue // the minute is newer than the trade
		}
		if r.HasChange {
			if open := r.Price / (1 + r.Change24/100); open > 0 {
				r.Change24 = 100 * (p.Price/open - 1)
			}
		}
		r.Price, r.At, r.Fast = p.Price, p.At/1000, true
		if p.N > 0 {
			r.N, r.Used, r.Spread = p.N, strings.Join(p.Used, ","), p.Spread
			r.Markets, r.Paths = p.Markets, strings.Join(p.Paths, ",")
		}
		s.Index[sym] = r
		n++
	}
	if b, ok := s.Index["BTC"]; ok {
		s.BTCUSD, s.BTCAt, s.BTCN, s.BTCSpread, s.BTCUsed = b.Price, b.At, b.N, b.Spread, b.Used
	}
	return n
}
