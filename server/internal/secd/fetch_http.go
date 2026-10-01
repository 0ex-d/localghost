package secd

// WHAT THE PHONE FETCHES FOR THE BOX, and what comes back. The box opens no connection, so the
// news feeds and the market tickers are fetched by the phone, the way it runs the web search
// round: /v1/fetch/list says what to fetch (the box owns the list), /v1/news/fetched and
// /v1/rates/fetched take the raw bodies and spool them whole for ghost.synthd and ghost.tallyd,
// and /v1/news and /v1/rates read back what the daemons made of them.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/egress"
	"github.com/LocalGhostDao/localghost/server/internal/feeds"
	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"github.com/LocalGhostDao/localghost/server/internal/monitor"
	"github.com/LocalGhostDao/localghost/server/internal/rates"
	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

// fetchListDoc is what the phone fetches.
type fetchListDoc struct {
	Feeds []feeds.Source `json:"feeds"`
	Rates []rates.Source `json:"rates"`
	// FeedsEvery is how often the feeds want fetching, in minutes; the tickers carry their own.
	FeedsEvery int `json:"feedsEvery"`
}

// newsDoc is the stories, with the marks for the screen's header.
type newsDoc struct {
	Stories    []hw.NewsStory `json:"stories"`
	LastFetch  int64          `json:"lastFetch"`
	LastDigest int64          `json:"lastDigest"`
	// Brief is the day's news in three or four sentences, written from the most-told stories'
	// summaries (the home screen's); "" before the first
	Brief   string `json:"brief"`
	BriefAt int64  `json:"briefAt"`
}

func (s *Server) mountedSlot() (int, bool) {
	s.mu.Lock()
	mounted := s.mounted
	s.mu.Unlock()
	return mounted, mounted >= 0
}

// handlePhoneNet , POST /v1/phone/net {"net":"wifi"|"mobile"|"none"} , the phone's word on its
// network, every quarter hour with its notification poll. While it says Wi-Fi the phone fetches
// the feeds and the tickers for the box (the box opens no connection); on mobile data, or when it
// falls silent, ghost.tallyd and ghost.synthd fetch for themselves (internal/egress).
func (s *Server) handlePhoneNet(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodPost {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	var q struct {
		Net string `json:"net"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&q) != nil {
		http.Error(w, "{\"net\":\"wifi|mobile|none\"}", http.StatusBadRequest)
		return
	}
	net, ok := egress.ParseNet(q.Net)
	if !ok {
		http.Error(w, "net is wifi, mobile or none", http.StatusBadRequest)
		return
	}
	if err := s.notif.SetSetting(mounted, "phone_net", net); err != nil {
		s.appearsDown(w)
		return
	}
	_ = s.notif.SetSetting(mounted, "phone_seen", egress.SeenString(time.Now()))
	w.Header().Set("Content-Type", "application/json")
	// the phone learns who fetches, so SETTINGS can say it
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "proxy": net == "wifi"})
}

// handleFetchList , GET /v1/fetch/list.
func (s *Server) handleFetchList(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodGet {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	// the phone's part of the rates: the ECB and the rank lists (the tickers are the box's own,
	// every minute, whatever the phone is on)
	doc := fetchListDoc{Rates: rates.PhoneSources(), FeedsEvery: int(feeds.FetchEvery / time.Minute)}
	if db, err := s.notif.DB(mounted); err == nil {
		if fl, err := hw.FeedList(db); err == nil {
			doc.Feeds = fl
		}
	}
	if doc.Feeds == nil {
		doc.Feeds = feeds.DefaultSources()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// spool writes a body whole under dir and names it into place, owned by the daemons' user.
func (s *Server) spool(dir, prefix string, body []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%d.json", prefix, time.Now().UnixNano()))
	if err := os.WriteFile(path+".part", body, 0o640); err != nil {
		return err
	}
	if s.cfg.RunUser != "" {
		if u, uerr := user.Lookup(s.cfg.RunUser); uerr == nil {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			_ = os.Chown(path+".part", uid, gid)
			_ = os.Chown(dir, uid, gid)
			_ = os.Chown(filepath.Dir(dir), uid, gid)
		}
	}
	if err := os.Rename(path+".part", path); err != nil {
		_ = os.Remove(path + ".part")
		return err
	}
	return nil
}

// handleNewsFetched , POST /v1/news/fetched , the feeds' bytes, as fetched, for ghost.synthd.
func (s *Server) handleNewsFetched(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodPost {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20)) // a dozen feeds at a few hundred KB each
	if err != nil || len(body) == 0 {
		s.appearsDown(w)
		return
	}
	var b struct {
		FetchedAt int64 `json:"fetchedAt"`
		Feeds     []struct {
			ID string `json:"id"`
		} `json:"feeds"`
	}
	if json.Unmarshal(body, &b) != nil || len(b.Feeds) == 0 {
		http.Error(w, "a news batch has feeds", http.StatusBadRequest)
		return
	}
	for _, f := range b.Feeds {
		if f.ID == "" || len(f.ID) > 40 {
			http.Error(w, "every feed has an id", http.StatusBadRequest)
			return
		}
	}
	if err := s.spool(filepath.Join(s.cfg.StateDir, "mnt", fmt.Sprintf("slot%d", mounted), "synthd", "news", "inbox"), "news", body); err != nil {
		s.appearsDown(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "feeds": len(b.Feeds)})
}

// handleRatesFetched , POST /v1/rates/fetched , the tickers' bodies for ghost.tallyd.
func (s *Server) handleRatesFetched(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodPost {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil || len(body) == 0 {
		s.appearsDown(w)
		return
	}
	b, ok := tally.ParseRates(body)
	if !ok {
		http.Error(w, "a rates batch has sources", http.StatusBadRequest)
		return
	}
	if err := s.spool(filepath.Join(s.cfg.StateDir, "mnt", fmt.Sprintf("slot%d", mounted), "tallyd", "rates"), "rates", body); err != nil {
		s.appearsDown(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "sources": len(b.Sources)})
}

// handleNews , GET /v1/news?since=<unix>&limit=N , the stories for the NEWS screen.
func (s *Server) handleNews(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodGet {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	db, err := s.notif.DB(mounted)
	if err != nil {
		s.appearsDown(w)
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if since <= 0 {
		since = time.Now().Add(-48 * time.Hour).Unix()
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	stories, err := hw.NewsStories(db, since, limit)
	if err != nil {
		secdLog.Warn("news read failed", "fn", "handleNews", "err", err)
		s.appearsDown(w)
		return
	}
	if stories == nil {
		stories = []hw.NewsStory{}
	}
	lf, ld := hw.NewsMarks(db)
	brief, briefAt := hw.NewsBrief(db)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(newsDoc{Stories: stories, LastFetch: lf, LastDigest: ld, Brief: brief, BriefAt: briefAt})
}

// handleRatesHistory , GET /v1/rates/history?code=BTC&days=N , a symbol's daily USD closes (the
// box's daily index) or a currency's daily ECB rate, newest first.
func (s *Server) handleRatesHistory(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodGet {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 8 {
		s.appearsDown(w)
		return
	}
	db, err := s.notif.DB(mounted)
	if err != nil {
		s.appearsDown(w)
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	var hist []hw.DayPrice
	if strings.EqualFold(code, rates.MarketIndexCode) {
		mh, err := tally.MarketHistory(db, days)
		if err != nil {
			s.appearsDown(w)
			return
		}
		for _, d := range mh {
			hist = append(hist, hw.DayPrice{Day: d.Day, Close: d.Value})
		}
	} else {
		var err error
		hist, err = hw.RatesHistory(db, code, days)
		if err != nil {
			s.appearsDown(w)
			return
		}
	}
	if hist == nil {
		hist = []hw.DayPrice{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": strings.ToUpper(code), "days": hist})
}

// handleRatesSeries , GET /v1/rates/series?code=BTC|CRYPTO50&res=1m|1h&hours=N , a symbol's (or
// the market index's) price every minute (the last week) or every hour (the last thirty days),
// oldest first: {ts, o, h, l, c, n, src}. src says how each point was made: live (the box's
// minute), minutes (an hour rolled up from them), venues (folded from the venues' candles, before
// the box was watching).
func (s *Server) handleRatesSeries(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodGet {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	q := r.URL.Query()
	code, res := q.Get("code"), q.Get("res")
	if res == "" {
		res = tally.ResHour
	}
	ri, ok := tally.Resolutions[res]
	if code == "" || len(code) > 10 || !ok {
		http.Error(w, "code=BTC|CRYPTO50 and res=1m|1h", http.StatusBadRequest)
		return
	}
	hours, _ := strconv.Atoi(q.Get("hours"))
	span := time.Duration(hours) * time.Hour
	if span <= 0 {
		span = 24 * time.Hour
		if res == tally.ResHour {
			span = ri.Window
		}
	}
	if span > ri.Keep {
		span = ri.Keep
	}
	db, err := s.notif.DB(mounted)
	if err != nil {
		s.appearsDown(w)
		return
	}
	now := time.Now()
	pts, err := tally.Series(db, code, res, now.Add(-span), now)
	if err != nil {
		s.appearsDown(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": strings.ToUpper(code), "res": res, "points": pts})
}

// handleRates , GET /v1/rates , the box's market numbers as they stand.
func (s *Server) handleRates(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodGet {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	db, err := s.notif.DB(mounted)
	if err != nil {
		s.appearsDown(w)
		return
	}
	snap, err := hw.RatesNow(db)
	if err != nil {
		secdLog.Warn("rates read failed", "fn", "handleRates", "err", err)
		s.appearsDown(w)
		return
	}
	if snap.Ranks == nil {
		snap.Ranks = []hw.CoinRow{}
	}
	// the market index rides with the snapshot: one number for crypto as a whole
	type withMarket struct {
		hw.RatesSnapshot
		Market *tally.MarketState `json:"market,omitempty"`
	}
	out := withMarket{RatesSnapshot: snap}
	if st, err := tally.MarketNow(db, time.Now()); err == nil {
		out.Market = &st
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// handleFeedsStatus , GET /v1/feeds/status , how the data the box pulls in is doing: the prices
// every minute, the exchanges, the history, the market index, the ECB, the rank lists, the daily
// candles and the news (internal/monitor). Box Status polls it while it is open.
func (s *Server) handleFeedsStatus(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || r.Method != http.MethodGet {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	db, err := s.notif.DB(mounted)
	if err != nil {
		s.appearsDown(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(monitor.Make(db, time.Now()))
}
