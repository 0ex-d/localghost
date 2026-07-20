// Package geo holds the reverse-geocoding TYPES. The machinery lives in framed's store, DB-backed:
// the first cut of this package was an in-RAM grid index, which forced filtering GeoNames down to
// what fit in memory , and "nearest town cities1000 happened to know" is not precision. The full
// dataset (allCountries, millions of rows , suburbs, villages, every named creek) belongs in
// postgres on the encrypted volume, where the nearest named point is typically within a few km and
// RAM does not care. See framed.Store.ImportGeo / ResolvePlace, and `ghost-cli ghost.framed
// geo-import`.
//
// The privacy stance is unchanged and is the reason this exists at all: Android's Geocoder and
// every tile server are network services , batch-geocoding an archive through them ships the
// person's multi-year location history to exactly the parties this box exists to exclude.
package geo

import "strings"

// Place is the resolved hierarchy. Empty fields mean "no data close enough", never a guess.
type Place struct {
	Continent string
	Country   string
	Admin1    string
	Admin2    string // county / regional district level, when admin2Codes data is loaded
	Locality  string // nearest populated place
	Park      string // nearest park/reserve within the park cap
	Feature   string // nearest physical feature (falls, peak, lake, trail) within the feature cap
}

// String renders the hierarchy the way a person would say it, most general first, empties skipped.
func (p Place) String() string {
	parts := make([]string, 0, 7)
	for _, s := range []string{p.Continent, p.Country, p.Admin1, p.Admin2, p.Locality, p.Park, p.Feature} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " / ")
}
