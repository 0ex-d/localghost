package tally

// HOW FAR THE WALKS HAVE COME, for Box Status. ghost.tallyd works it out once a minute, after the
// minute's pages (it holds the markets the venues were seen quoting; working that out again on
// every status read would cost each read a scan of two days of quotes), and keeps it in settings
// (tally_progress); internal/monitor reads it back.

import (
	"encoding/json"
	"math"
	"strconv"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/rates"
)

// HistoryState is the walk back at one resolution (thirty days of hours, seven of minutes).
type HistoryState struct {
	Res       string  `json:"res"`
	Markets   int     `json:"markets"`   // markets walked back: the USDT leg, two venues per symbol
	Walking   int     `json:"walking"`   // of those, still being walked
	PagesLeft int     `json:"pagesLeft"` // from the oldest bar held to the window's start
	Covered   float64 `json:"covered"`   // the share of the window held, averaged over the markets
	Oldest    int64   `json:"oldest"`    // the oldest bar held
	Folded    bool    `json:"folded"`    // the market index has been made over the history
}

// BackfillState is the walk back of the daily candles, to 2010.
type BackfillState struct {
	Markets   int    `json:"markets"`
	Done      int    `json:"done"`
	OldestDay string `json:"oldestDay,omitempty"`
	Next      string `json:"next,omitempty"`
}

// Progress is the whole of it.
type Progress struct {
	At         int64          `json:"at"`
	Symbols    int            `json:"symbols"`    // followed
	Seen       int            `json:"seen"`       // markets the venues were seen quoting (two days)
	DailyFresh int            `json:"dailyFresh"` // of those, daily candles fetched in the last day
	History    []HistoryState `json:"history"`
	Backfill   BackfillState  `json:"backfill"`
}

const progressKey = "tally_progress"

// MakeProgress works the progress out. why is the backfill's last word (NextBackfill's).
func MakeProgress(db *poltergres.ReadWrite, symbols []string, seen []rates.Market, now time.Time, why string) Progress {
	p := Progress{At: now.Unix(), Symbols: len(symbols), Seen: len(seen)}
	marks := Marks(db)
	for _, m := range seen {
		if now.Unix()-marks["hist:"+m.ID()] < 86400 {
			p.DailyFresh++
		}
	}
	for _, res := range []string{ResHour, ResMinute} {
		p.History = append(p.History, HistoryProgress(db, res, symbols, seen, now))
	}
	done := map[string]bool{}
	if rows, err := db.Query("SELECT key FROM settings WHERE key LIKE 'hist_done_%'"); err == nil {
		for _, v := range rows.Vals {
			if len(v) > 0 && v[0] != nil {
				done[(*v[0])[len("hist_done_"):]] = true
			}
		}
	}
	p.Backfill.Markets = len(seen)
	for _, m := range seen {
		if done[m.ID()] {
			p.Backfill.Done++
		}
	}
	if rows, err := db.Query("SELECT min(day) FROM crypto_daily"); err == nil && len(rows.Vals) == 1 {
		p.Backfill.OldestDay = deref(rows.Vals[0][0])
	}
	p.Backfill.Next = why
	return p
}

// HistoryProgress is one resolution's walk: per market the oldest bar held (one primary-key
// lookup each), against the window.
func HistoryProgress(db *poltergres.ReadWrite, res string, symbols []string, seen []rates.Market, now time.Time) HistoryState {
	st := HistoryState{Res: res}
	ri, ok := Resolutions[res]
	if !ok {
		return st
	}
	done := barsDone(db, res)
	floor := now.Add(-ri.Window).Truncate(ri.Step).Unix()
	span := now.Unix() - floor
	step := int64(ri.Step / time.Second)
	var sum float64
	ms := barMarkets(res, symbols, seen)
	for _, m := range ms {
		var oldest int64
		if rows, err := db.Query("SELECT coalesce(min(ts),0) FROM crypto_bars WHERE res = $1 AND exchange = $2 AND base = $3 AND quote = $4", res, m.Exchange, m.Base, m.Quote); err == nil && len(rows.Vals) == 1 {
			oldest, _ = strconv.ParseInt(deref(rows.Vals[0][0]), 10, 64)
		}
		if oldest > 0 && (st.Oldest == 0 || oldest < st.Oldest) {
			st.Oldest = oldest
		}
		cov := 0.0
		if oldest > 0 {
			cov = math.Min(1, math.Max(0, float64(now.Unix()-oldest)/float64(span)))
		}
		if !done[barsDoneKey(res, m)] && (oldest == 0 || oldest > floor) {
			st.Walking++
			left := span
			if oldest > 0 {
				left = oldest - floor
			}
			per := int64(m.PageBars()) * step
			if per > 0 {
				st.PagesLeft += int((left + per - 1) / per)
			}
		} else if oldest > 0 && oldest <= floor {
			cov = 1
		}
		sum += cov
	}
	st.Markets = len(ms)
	if len(ms) > 0 {
		st.Covered = sum / float64(len(ms))
	}
	if rows, err := db.Query("SELECT 1 FROM settings WHERE key = $1", "series_folded_"+res); err == nil {
		st.Folded = len(rows.Vals) == 1
	}
	return st
}

// SaveProgress keeps it for the status reads.
func SaveProgress(db *poltergres.ReadWrite, p Progress) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return db.Exec("INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value", progressKey, string(b))
}

// LoadProgress reads it back; false before ghost.tallyd's first minute.
func LoadProgress(db *poltergres.ReadWrite) (Progress, bool) {
	var p Progress
	rows, err := db.Query("SELECT value FROM settings WHERE key = $1", progressKey)
	if err != nil || len(rows.Vals) != 1 || rows.Vals[0][0] == nil {
		return p, false
	}
	if json.Unmarshal([]byte(*rows.Vals[0][0]), &p) != nil {
		return p, false
	}
	return p, true
}
