package pgtest

import (
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/cued"
)

// cued's "something new near you": a fresh trail point, a taste for beaches, three beaches around,
// one with a photo of yours, one walked past on the trail, and the third is offered; offered once.
func TestOfferNearbyAgainstPostgres(t *testing.T) {
	db := fresh(t)
	now := time.Now()
	ins := func(q string, args ...any) {
		t.Helper()
		if err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// nothing yet: no trail
	if n, why, err := cued.OfferNearby(db, "", now); err != nil || n != nil || !strings.Contains(why, "no trail") {
		t.Fatalf("%v %q %v", n, why, err)
	}
	ins(`INSERT INTO location_points (ts, lat, lon, source) VALUES ($1, 39.200, 20.150, 'phone-ab')`, now.Unix()-600)
	// no taste
	if n, why, _ := cued.OfferNearby(db, "", now); n != nil || !strings.Contains(why, "no taste") {
		t.Fatalf("%v %q", n, why)
	}
	ins(`INSERT INTO settings (key, value) VALUES ('synthd_taste', $1)`,
		`{"days":10,"photos":100,"interests":[{"name":"beaches","weight":0.6,"tags":["beach","sea"]}]}`)
	// three beaches within a few km, a village (not a taste), and one spot 40 km off
	for i, sp := range []struct {
		id       int
		name, fc string
		lat, lon float64
		kind     string
	}{
		{1, "Voutoumi", "BCH", 39.215, 20.170, "S"},
		{2, "Vrika", "BCH", 39.190, 20.130, "S"},
		{3, "Marmari", "BCH", 39.230, 20.120, "S"},
		{4, "Gaios", "PPL", 39.198, 20.185, "P"},
		{5, "Far Beach", "BCH", 39.600, 20.150, "S"},
	} {
		_ = i
		ins(`INSERT INTO geo_points (geonameid, name, lat, lon, kind, fcode, country) VALUES ($1,$2,$3,$4,$5,$6,'GR')`, sp.id, sp.name, sp.lat, sp.lon, sp.kind, sp.fc)
	}
	// a photo at Voutoumi: been there; the trail passed Vrika
	ins(`INSERT INTO frames (hash, archive_path, has_gps, lat, lon, kind, taken_at) VALUES ('a', '/a', true, 39.2151, 20.1702, 'photo', $1)`, now.Unix()-86400)
	ins(`INSERT INTO location_points (ts, lat, lon, source) VALUES ($1, 39.1901, 20.1301, 'phone-ab')`, now.Unix()-7200)

	n, key, err := cued.OfferNearby(db, "", now)
	if err != nil || n == nil {
		t.Fatalf("offer %v %q %v", n, key, err)
	}
	if n.Kind != "nearby" || n.Service != "ghost.cued" || !strings.Contains(n.Title, "Marmari") || !strings.Contains(n.Body, "new to you") || !strings.Contains(n.Body, "km") {
		t.Fatalf("offer %+v", n)
	}
	if !strings.HasPrefix(key, "marmari|BCH|") {
		t.Fatalf("key %q", key)
	}
	// offered once: with Marmari remembered, nothing else fits
	sent := cued.RememberSent("", key)
	if n2, why, _ := cued.OfferNearby(db, sent, now); n2 != nil || !strings.Contains(why, "nothing new") {
		t.Fatalf("second: %v %q", n2, why)
	}
	// a stale trail point offers nothing
	if n3, why, _ := cued.OfferNearby(db, "", now.Add(2*time.Hour)); n3 != nil || !strings.Contains(why, "old") {
		t.Fatalf("stale: %v %q", n3, why)
	}
	// the sent list is capped and deduplicated
	s := ""
	for i := 0; i < 70; i++ {
		s = cued.RememberSent(s, "k"+string(rune('a'+i%26))+strings.Repeat("x", i/26))
	}
	if len(strings.Split(s, "\n")) > 60 {
		t.Fatal("sent list grows without bound")
	}
}
