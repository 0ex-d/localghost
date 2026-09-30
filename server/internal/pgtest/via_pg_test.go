package pgtest

import (
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/framed"
	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// How the phone took each point is stored with it, read back by the trail report's query, and
// counted in the framed drill-in with the newest point's age.
func TestViaStoredAndSummarised(t *testing.T) {
	db := fresh(t)
	fs := framed.NewStoreDB(db)
	now := time.Now().Unix()
	pts := []framed.SpooledPoint{
		{TrackPoint: framed.TrackPoint{TS: now - 600, Lat: 39.2, Lon: 20.1}, Via: "w"},
		{TrackPoint: framed.TrackPoint{TS: now - 300, Lat: 39.21, Lon: 20.1}, Via: "p"},
		{TrackPoint: framed.TrackPoint{TS: now - 180, Lat: 39.22, Lon: 20.1}, Via: "w"},
	}
	if err := fs.InsertSpooled("phone-ab", pts); err != nil {
		t.Fatal(err)
	}
	// the same second again is absorbed, not doubled
	if err := fs.InsertSpooled("phone-ab", pts[:1]); err != nil {
		t.Fatal(err)
	}
	rows, err := fs.TrailRows(now-3600, now+1)
	if err != nil || len(rows) != 3 || rows[1].Via != "p" || rows[0].Source != "phone-ab" {
		t.Fatalf("rows %+v %v", rows, err)
	}
	got := map[string]string{}
	for _, kv := range hw.DaemonSummaryFrom(db, "ghost.framed") {
		got[kv.K] = kv.V
	}
	if got["trail, last 24 h"] != "2 quarter-hour · 1 other apps' fixes" || got["newest track point"] != "3 min ago" {
		t.Fatalf("summary %v", got)
	}
}
