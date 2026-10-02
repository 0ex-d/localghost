package cued

// SOMETHING NEW NEAR YOU. The box knows what the person likes to photograph (synthd's taste), the
// places around any point (the GeoNames spots ghost.framed imported), where they have been (their
// photos, and their trail), and where they are now (the trail's newest point). Put together, that
// is a suggestion the MEMORIES near-you card already makes on request; this makes it once a day
// without being asked, when the phone is somewhere and the taste says a place nearby would suit.
// Pure over a Querier, so the tests run it against Postgres.

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"github.com/LocalGhostDao/localghost/server/internal/outings"
)

const (
	nearbyFreshS   = 45 * 60 // the trail's newest point must be this recent: the phone is there now
	nearbyRadiusKm = 12.0    // how far around the phone to look
	nearbyMinKm    = 0.3     // and no closer than this: a spot under the phone is where they are
	nearbyTrailM   = 200.0   // a trail point this close to a spot means they have been
	nearbySentKeep = 60      // spots offered, remembered (the newest kept)
)

// OfferNearby looks around the phone's newest trail point and returns the notification to post,
// or nil and why not. sent is the settings value of what was offered before (RememberSent's
// output); the returned key is what to remember when the offer is posted.
func OfferNearby(db hw.Querier, sent string, now time.Time) (n *hw.Notification, key string, err error) {
	// where the phone is now
	rows, err := db.Query("SELECT ts, lat, lon FROM location_points ORDER BY ts DESC LIMIT 1")
	if err != nil {
		return nil, "", err
	}
	if len(rows.Vals) == 0 || rows.Vals[0][0] == nil {
		return nil, "no trail point yet", nil
	}
	ts, _ := strconv.ParseInt(*rows.Vals[0][0], 10, 64)
	lat, _ := strconv.ParseFloat(*rows.Vals[0][1], 64)
	lon, _ := strconv.ParseFloat(*rows.Vals[0][2], 64)
	if now.Unix()-ts > nearbyFreshS {
		return nil, "the newest trail point is " + time.Duration(now.Unix()-ts).Truncate(time.Minute).String() + " old", nil
	}
	// what they like
	rows, err = db.Query("SELECT value FROM settings WHERE key = 'synthd_taste'")
	if err != nil {
		return nil, "", err
	}
	var taste outings.Taste
	if len(rows.Vals) == 0 || rows.Vals[0][0] == nil || json.Unmarshal([]byte(*rows.Vals[0][0]), &taste) != nil || len(taste.Interests) == 0 {
		return nil, "no taste yet (synthd builds it from the tagged photos)", nil
	}
	// the places around, ranked
	spots, err := hw.QuerySpotsNear(db, lat, lon, nearbyRadiusKm)
	if err != nil {
		return nil, "", err
	}
	if len(spots) == 0 {
		return nil, "no places known within " + strconv.Itoa(int(nearbyRadiusKm)) + " km", nil
	}
	cells, err := hw.QueryPhotoCellsNear(db, lat, lon, nearbyRadiusKm)
	if err != nil {
		cells = nil
	}
	ranked := outings.Rank(spots, taste, lat, lon, nearbyRadiusKm, hw.PhotosNearFunc(cells))
	// their own trail around here: a place walked past is not new
	trail, err := trailPointsNear(db, lat, lon, nearbyRadiusKm)
	if err != nil {
		trail = nil
	}
	seen := sentSet(sent)
	for _, s := range ranked {
		if s.BeenThere > 0 || s.DistanceKm < nearbyMinKm {
			continue
		}
		k := spotKey(s.Spot)
		if seen[k] {
			continue
		}
		if visited(trail, s.Lat, s.Lon) {
			continue
		}
		what := s.Name
		if s.Kind != "" {
			what += " (" + s.Kind + ")"
		}
		dist := fmt.Sprintf("%.1f km", s.DistanceKm)
		if s.DistanceKm >= 10 {
			dist = fmt.Sprintf("%.0f km", s.DistanceKm)
		}
		return &hw.Notification{
			Service: "ghost.cued", Kind: "nearby", Link: "memories:near",
			Title: "something new near you: " + s.Name,
			Body:  what + ", " + dist + " " + s.Bearing + " of where you are , " + s.Why + ". More under MEMORIES › near you.",
		}, k, nil
	}
	return nil, "nothing new around here that fits", nil
}

// trailPointsNear is the person's own trail points within km of a point (a bounding box; the
// distance test is the caller's).
func trailPointsNear(db hw.Querier, lat, lon, km float64) ([][2]float64, error) {
	dLat := km / 111.0
	cosLat := math.Cos(lat * math.Pi / 180)
	if cosLat < 0.05 {
		cosLat = 0.05
	}
	dLon := km / (111.0 * cosLat)
	rows, err := db.Query("SELECT lat, lon FROM location_points WHERE lat BETWEEN $1 AND $2 AND lon BETWEEN $3 AND $4 LIMIT 20000",
		lat-dLat, lat+dLat, lon-dLon, lon+dLon)
	if err != nil {
		return nil, err
	}
	out := make([][2]float64, 0, len(rows.Vals))
	for _, r := range rows.Vals {
		if len(r) == 2 && r[0] != nil && r[1] != nil {
			a, _ := strconv.ParseFloat(*r[0], 64)
			b, _ := strconv.ParseFloat(*r[1], 64)
			out = append(out, [2]float64{a, b})
		}
	}
	return out, nil
}

// visited: a trail point within nearbyTrailM of the spot.
func visited(trail [][2]float64, lat, lon float64) bool {
	for _, p := range trail {
		if haversineM(p[0], p[1], lat, lon) <= nearbyTrailM {
			return true
		}
	}
	return false
}

func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := (lat2-lat1)*math.Pi/180, (lon2-lon1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// spotKey names a spot for the sent list: GeoNames ids are not selected, so name, code and a
// rounded position.
func spotKey(s outings.Spot) string {
	return fmt.Sprintf("%s|%s|%.3f|%.3f", strings.ToLower(s.Name), s.FCode, s.Lat, s.Lon)
}

func sentSet(sent string) map[string]bool {
	out := map[string]bool{}
	for _, k := range strings.Split(sent, "\n") {
		if k != "" {
			out[k] = true
		}
	}
	return out
}

// RememberSent adds key to the sent list, newest last, keeping the last nearbySentKeep.
func RememberSent(sent, key string) string {
	var keys []string
	for _, k := range strings.Split(sent, "\n") {
		if k != "" && k != key {
			keys = append(keys, k)
		}
	}
	keys = append(keys, key)
	if len(keys) > nearbySentKeep {
		keys = keys[len(keys)-nearbySentKeep:]
	}
	return strings.Join(keys, "\n")
}
