package hw

import (
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/poltergres"
	"github.com/LocalGhostDao/localghost/server/internal/tally"
)

// RatesDoc is /v1/rates: the snapshot and the market index riding with it (one number for crypto
// as a whole). tallyd writes it to Redis every minute (HotRates); secd answers from there.
type RatesDoc struct {
	RatesSnapshot
	Market  *tally.MarketState `json:"market,omitempty"`
	BuiltAt int64              `json:"builtAt"`
}

// RatesDocNow reads /v1/rates from Postgres.
func RatesDocNow(db *poltergres.ReadWrite, now time.Time) (RatesDoc, error) {
	snap, err := RatesNow(db)
	if err != nil {
		return RatesDoc{}, err
	}
	if snap.Ranks == nil {
		snap.Ranks = []CoinRow{}
	}
	d := RatesDoc{RatesSnapshot: snap, BuiltAt: now.Unix()}
	if st, err := tally.MarketNow(db, now); err == nil {
		d.Market = &st
	}
	return d, nil
}
