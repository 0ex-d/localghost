package tally

// THE HISTORY, a page a tick: for each market the box follows, the daily candles are walked back
// from the oldest day held, one venue page at a time, until the venue has nothing older or the
// floor is reached. Where it left off is the table itself (the oldest day), so a restart or a
// week with the phone as proxy loses nothing.

import (
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/rates"
)

// BackfillFloor is the earliest day asked for.
const BackfillFloor = "2010-01-01"

// NextBackfill picks, among the markets given (the pairs the venues were seen quoting), the one
// whose history wants another page, and the page: from the oldest candle held back one venue
// page, down to the floor. nil when every market is done or has no candle yet (the daily fetch
// brings the first week; the walk starts from there).
func NextBackfill(db *poltergres.ReadWrite, markets []rates.Market, now time.Time) (m *rates.Market, from, to time.Time, why string) {
	done := map[string]bool{}
	if rows, err := db.Query("SELECT key FROM settings WHERE key LIKE 'hist_done_%'"); err == nil {
		for _, v := range rows.Vals {
			if len(v) > 0 && v[0] != nil {
				done[strings.TrimPrefix(*v[0], "hist_done_")] = true
			}
		}
	}
	floor, _ := time.Parse("2006-01-02", BackfillFloor)
	for _, mk := range markets {
		if done[mk.ID()] {
			continue
		}
		oldest := OldestDay(db, mk)
		if oldest == "" {
			continue
		}
		if oldest <= BackfillFloor {
			MarkHistoryDone(db, mk)
			continue
		}
		oldDay, err := time.Parse("2006-01-02", oldest)
		if err != nil {
			MarkHistoryDone(db, mk)
			continue
		}
		to = oldDay.AddDate(0, 0, -1)
		from = to.AddDate(0, 0, -mk.PageDays()+1)
		if from.Before(floor) {
			from = floor
		}
		m := new(rates.Market)
		*m = mk
		return m, from, to.Add(24*time.Hour - time.Second), "walking " + mk.ID() + " back from " + oldest
	}
	return nil, time.Time{}, time.Time{}, "every market's history is as far back as its venue goes"
}

// MarkHistoryDone records that a venue has nothing older for a market.
func MarkHistoryDone(db *poltergres.ReadWrite, m rates.Market) {
	_ = db.Exec("INSERT INTO settings (key, value) VALUES ($1, '1') ON CONFLICT (key) DO NOTHING", "hist_done_"+m.ID())
}
