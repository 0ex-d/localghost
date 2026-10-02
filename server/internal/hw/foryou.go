package hw

// FOR YOU, on home: what the box knows that would interest this person today, from what it
// holds and nothing else. Three kinds:
//   - places near where the trail last was (within six hours), ranked by what the person
//     photographs (synthd's taste over the GeoNames spots, as MEMORIES › near you does), the
//     ones they have no photos near first;
//   - this day in earlier years: the day stories and outings of the same date, and the days
//     with photos, each opening the map on that day;
//   - the day's news that touches what the person has said about themselves (the 'me' memories
//     and the distilled ones' titles), the stories the brief already tells left out.
// Read by secd for home's snapshot and /v1/home, kept in Redis (hot:foryou) for a while.

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/outings"
)

// ForYouPlace is a place near where the trail last was.
type ForYouPlace struct {
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	Km      float64 `json:"km"`
	Bearing string  `json:"bearing"`
	Why     string  `json:"why"`
	New     bool    `json:"new"` // no photos of theirs within a kilometre
}

// ForYouDay is this date in an earlier year.
type ForYouDay struct {
	Day      string `json:"day"` // 2024-10-02
	YearsAgo int    `json:"yearsAgo"`
	MemoryID int64  `json:"memoryId,omitempty"` // the day story or outing, when there is one
	Title    string `json:"title"`
	Lead     string `json:"lead"`
	Photos   int    `json:"photos"`
	Place    string `json:"place,omitempty"`
}

// ForYouStory is a story of the day that touches something the person said about themselves.
type ForYouStory struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Lead  string `json:"lead"`
	Why   string `json:"why"` // "you mention sailing, Romania"
}

// ForYou is home's FOR YOU card.
type ForYou struct {
	At      int64         `json:"at"`
	From    int64         `json:"from"` // the trail point the places are measured from, 0 for none
	Places  []ForYouPlace `json:"places"`
	Days    []ForYouDay   `json:"days"`
	Stories []ForYouStory `json:"stories"`
	Note    string        `json:"note,omitempty"` // why there are no places
	lat     float64
	lon     float64
}

// ForYouFresh is how long a FOR YOU is kept; ForYouMoved is how far the trail moves before the
// places are looked up again.
const (
	ForYouFresh   = 20 * time.Minute
	ForYouMovedKm = 1.0
	forYouTrailS  = 6 * 3600 // the trail's newest point, at most this old, for places
	forYouKm      = 10.0
)

// HotForYou is the Redis copy.
const HotForYou = "hot:foryou"

// forYouCopy is the Redis copy with where the places were measured from.
type forYouCopy struct {
	ForYou
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// MarshalCopy and UnmarshalCopy carry the position with the Redis copy (never to the phone).
func (f ForYou) MarshalCopy() ([]byte, error) {
	return json.Marshal(forYouCopy{ForYou: f, Lat: f.lat, Lon: f.lon})
}

// UnmarshalForYouCopy reads the Redis copy.
func UnmarshalForYouCopy(b []byte) (ForYou, bool) {
	var c forYouCopy
	if json.Unmarshal(b, &c) != nil || c.At == 0 {
		return ForYou{}, false
	}
	f := c.ForYou
	f.lat, f.lon = c.Lat, c.Lon
	return f, true
}

// TrailNewest is the trail's newest point: when and where; ts 0 when there is none.
func TrailNewest(c Querier) (ts int64, lat, lon float64) {
	rows, err := c.Query("SELECT ts, lat, lon FROM location_points ORDER BY ts DESC LIMIT 1")
	if err != nil || len(rows.Vals) == 0 || len(rows.Vals[0]) < 3 || rows.Vals[0][0] == nil {
		return 0, 0, 0
	}
	ts, _ = strconv.ParseInt(deref(rows.Vals[0][0]), 10, 64)
	lat, _ = strconv.ParseFloat(deref(rows.Vals[0][1]), 64)
	lon, _ = strconv.ParseFloat(deref(rows.Vals[0][2]), 64)
	return ts, lat, lon
}

// StillFresh says whether a kept FOR YOU still stands: young enough, and the trail not moved
// far from where its places were measured (the trail's newest point given).
func (f ForYou) StillFresh(now time.Time, ts int64, lat, lon float64) bool {
	if now.Unix()-f.At > int64(ForYouFresh.Seconds()) {
		return false
	}
	if ts == 0 || ts == f.From {
		return true
	}
	if now.Unix()-ts > forYouTrailS {
		return f.From == 0 || now.Unix()-f.From > forYouTrailS
	}
	if f.From == 0 {
		return false // a fresh point where there was none
	}
	return distKm(f.lat, f.lon, lat, lon) < ForYouMovedKm
}

// ForYouNow builds the card. nd is the news as it stands (nil for none).
func ForYouNow(c Querier, nd *NewsDoc, now time.Time) ForYou {
	f := ForYou{At: now.Unix(), Places: []ForYouPlace{}, Days: []ForYouDay{}, Stories: []ForYouStory{}}
	ts, lat, lon := TrailNewest(c)
	f.Places, f.Note = placesNear(c, now, ts, lat, lon)
	if ts != 0 && now.Unix()-ts <= forYouTrailS {
		f.From, f.lat, f.lon = ts, lat, lon
	}
	f.Days = onThisDay(c, now)
	if nd != nil {
		f.Stories = PickStories(nd.Stories, InterestTerms(aboutMeTexts(c)), nd.BriefStories, now)
	}
	return f
}

// placesNear ranks the places around the trail's newest point by the taste.
func placesNear(c Querier, now time.Time, ts int64, lat, lon float64) ([]ForYouPlace, string) {
	out := []ForYouPlace{}
	if ts == 0 {
		return out, "no trail yet"
	}
	if now.Unix()-ts > forYouTrailS {
		return out, "the trail's newest point is " + strconv.FormatInt((now.Unix()-ts)/3600, 10) + " h old"
	}
	var taste outings.Taste
	rows, err := c.Query("SELECT value FROM settings WHERE key = 'synthd_taste'")
	if err != nil || len(rows.Vals) == 0 || rows.Vals[0][0] == nil || json.Unmarshal([]byte(*rows.Vals[0][0]), &taste) != nil || len(taste.Interests) == 0 {
		return out, "no taste yet (the box builds it from the tagged photos)"
	}
	spots, err := QuerySpotsNear(c, lat, lon, forYouKm)
	if err != nil || len(spots) == 0 {
		return out, "no places known around here"
	}
	cells, _ := QueryPhotoCellsNear(c, lat, lon, forYouKm)
	ranked := outings.Rank(spots, taste, lat, lon, forYouKm, PhotosNearFunc(cells))
	// the new ones first, the rank's order within each; a spot under the phone is where they are
	sort.SliceStable(ranked, func(i, j int) bool { return (ranked[i].BeenThere == 0) && (ranked[j].BeenThere > 0) })
	for _, s := range ranked {
		if s.DistanceKm < 0.3 {
			continue
		}
		out = append(out, ForYouPlace{Name: s.Name, Kind: s.Kind, Km: math.Round(s.DistanceKm*10) / 10, Bearing: s.Bearing, Why: s.Why, New: s.BeenThere == 0})
		if len(out) == 3 {
			break
		}
	}
	if len(out) == 0 {
		return out, "nothing around here that fits what you photograph"
	}
	return out, ""
}

// onThisDay is this date in earlier years: the day story or outing of it, and its photos.
func onThisDay(c Querier, now time.Time) []ForYouDay {
	out := []ForYouDay{}
	loc := LocalZone(c)
	today := now.In(loc)
	md := today.Format("01-02")
	byDay := map[string]*ForYouDay{}
	add := func(day string) *ForYouDay {
		if d, ok := byDay[day]; ok {
			return d
		}
		y, err := strconv.Atoi(day[:4])
		if err != nil || y >= today.Year() {
			return nil
		}
		d := &ForYouDay{Day: day, YearsAgo: today.Year() - y}
		byDay[day] = d
		return d
	}
	if rows, err := c.Query(`SELECT id, title, body, source_ref FROM memories WHERE NOT tombstoned AND kind IN ('day','outing')
		AND (source_ref LIKE 'day:%-' || $1 OR source_ref LIKE 'outing:%-' || $1) ORDER BY source_ref DESC LIMIT 12`, md); err == nil {
		for _, v := range rows.Vals {
			if len(v) < 4 {
				continue
			}
			ref := deref(v[3])
			day := ref[strings.IndexByte(ref, ':')+1:]
			if len(day) != 10 {
				continue
			}
			d := add(day)
			if d == nil || (d.MemoryID != 0 && strings.HasPrefix(ref, "outing:")) {
				continue // the day story first, an outing when there is none
			}
			d.MemoryID, _ = strconv.ParseInt(deref(v[0]), 10, 64)
			d.Title, d.Lead = deref(v[1]), firstSentence(deref(v[2]))
		}
	}
	// the photos of the date, by the local day they were taken (the zone by name; the box's own
	// clock has none, so its offset now)
	zone, shift := loc.String(), 0
	if zone == "Local" || zone == "" {
		_, shift = now.In(loc).Zone()
		zone = "UTC"
	}
	if rows, err := c.Query(`SELECT to_char(to_timestamp(taken_at + $4) AT TIME ZONE $1, 'YYYY-MM-DD') AS d, count(*), max(place)
		FROM frames WHERE kind = 'photo' AND taken_at > 0 AND taken_at < $2
		AND to_char(to_timestamp(taken_at + $4) AT TIME ZONE $1, 'MM-DD') = $3 GROUP BY d ORDER BY d DESC LIMIT 6`,
		zone, now.Add(-300*24*time.Hour).Unix(), md, shift); err == nil {
		for _, v := range rows.Vals {
			if len(v) < 3 {
				continue
			}
			if d := add(deref(v[0])); d != nil {
				d.Photos, _ = strconv.Atoi(deref(v[1]))
				d.Place = lastPart(deref(v[2]))
			}
		}
	}
	for _, d := range byDay {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].YearsAgo < out[j].YearsAgo })
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// lastPart is a place hierarchy's most precise part ("Greece / Corfu / Kassiopi" is Kassiopi).
func lastPart(place string) string {
	p := strings.Split(place, " / ")
	for i := len(p) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(p[i]); t != "" {
			return t
		}
	}
	return ""
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i > 0 && i < 220 {
		return s[:i+1]
	}
	if len(s) > 220 {
		return strings.TrimSpace(s[:220]) + "…"
	}
	return s
}

// aboutMeTexts is what the person said about themselves: the 'me' memories (twice the weight)
// and the titles of the distilled ones, the newest three hundred.
func aboutMeTexts(c Querier) []WeightedText {
	var out []WeightedText
	if rows, err := c.Query("SELECT title || '. ' || body FROM memories WHERE kind = 'me' AND NOT tombstoned LIMIT 80"); err == nil {
		for _, v := range rows.Vals {
			out = append(out, WeightedText{Text: deref(v[0]), Weight: 2})
		}
	}
	if rows, err := c.Query("SELECT title FROM memories WHERE kind = 'distilled' AND NOT tombstoned ORDER BY created_at DESC LIMIT 300"); err == nil {
		for _, v := range rows.Vals {
			out = append(out, WeightedText{Text: deref(v[0]), Weight: 1})
		}
	}
	if rows, err := c.Query("SELECT value FROM settings WHERE key = 'owner_name'"); err == nil && len(rows.Vals) == 1 {
		out = append(out, WeightedText{Text: deref(rows.Vals[0][0]), Weight: -1}) // the name is never a match
	}
	return out
}

// WeightedText is a text and how much its words count.
type WeightedText struct {
	Text   string
	Weight int
}

var (
	reWord      = regexp.MustCompile(`[\p{L}][\p{L}\p{N}'’-]*`)
	interestOff = map[string]bool{}
)

func init() {
	for _, w := range strings.Fields(`about above after again against also always among another anything around because been before being below
		between both could does doing down during each every from further have having here into just like likes made make many more most much
		must never only other over own same should some such than that their them then there these they this those through under until very
		want wants were what when where which while will with would your yours ours mine myself himself herself prefers prefer preference
		lives live living works work working uses using used plans plan planned went going goes visit visited favourite favorite thinks think
		thought said says enjoys enjoy loves love likes recently often usually year years month months week weeks today time times people
		person thing things something someone first last next good great best well really still part based home also told tell called name
		memory memories fact facts note notes day days`) {
		interestOff[w] = true
	}
}

func termKey(w string) string {
	w = strings.ToLower(strings.Trim(w, "'’-"))
	w = strings.TrimSuffix(strings.TrimSuffix(w, "'s"), "’s")
	if len(w) > 4 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
		w = w[:len(w)-1]
	}
	return w
}

// InterestTerms is the words the person uses about themselves that could match a story: a word
// of four letters or more, not a common one, counted by the weight of the texts it is in (a 'me'
// memory 2, a distilled title 1), kept from 2 up; the display form is the first as written. A
// text of weight -1 (the owner's name) takes its words out (pure, for the tests).
func InterestTerms(texts []WeightedText) map[string]string {
	score := map[string]int{}
	shown := map[string]string{}
	never := map[string]bool{}
	for _, t := range texts {
		seen := map[string]bool{}
		for _, w := range reWord.FindAllString(t.Text, -1) {
			k := termKey(w)
			if t.Weight < 0 {
				never[k] = true
				continue
			}
			if len(k) < 4 || interestOff[k] || seen[k] {
				continue
			}
			seen[k] = true
			score[k] += t.Weight
			if _, ok := shown[k]; !ok {
				shown[k] = strings.Trim(w, "'’-")
			}
		}
	}
	out := map[string]string{}
	for k, s := range score {
		if s >= 2 && !never[k] {
			out[k] = shown[k]
		}
	}
	return out
}

// PickStories is the day's stories that share words with what the person said about themselves,
// the most shared and most told first, three at most, the brief's own stories left out (pure,
// for the tests).
func PickStories(stories []NewsStory, terms map[string]string, skip []int64, now time.Time) []ForYouStory {
	out := []ForYouStory{}
	if len(terms) == 0 {
		return out
	}
	off := map[int64]bool{}
	for _, id := range skip {
		off[id] = true
	}
	type cand struct {
		s     NewsStory
		why   []string
		score int
	}
	var cs []cand
	for _, s := range stories {
		if off[s.ID] || s.Title == "" || now.Unix()-s.LastSeen > 36*3600 {
			continue
		}
		seen := map[string]bool{}
		var why []string
		for _, w := range reWord.FindAllString(s.Title+" "+leadOf(s.Summary), -1) {
			k := termKey(w)
			if d, ok := terms[k]; ok && !seen[k] {
				seen[k] = true
				why = append(why, d)
			}
		}
		if len(why) > 0 {
			cs = append(cs, cand{s: s, why: why, score: len(why)*4 + min(s.Sources, 6)})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].score != cs[j].score {
			return cs[i].score > cs[j].score
		}
		return cs[i].s.LastSeen > cs[j].s.LastSeen
	})
	for _, c := range cs {
		if len(c.why) > 3 {
			c.why = c.why[:3]
		}
		out = append(out, ForYouStory{ID: c.s.ID, Title: c.s.Title, Lead: leadOf(c.s.Summary), Why: "you mention " + strings.Join(c.why, ", ")})
		if len(out) == 3 {
			break
		}
	}
	return out
}

func distKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := (lat2-lat1)*math.Pi/180, (lon2-lon1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}
