package framed

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// a point dLat degrees north (about 111 km a degree) and dLon east of a start, at minute m
func at(m int, lat, lon float64) TrackPoint {
	return TrackPoint{TS: 1790000000 + int64(m)*60, Lat: lat, Lon: lon}
}

const dayStart, dayEnd = 1789990000, 1790090000

func TestAGlitchIsAskedAbout(t *testing.T) {
	// a walk in London, and one fix 60 km away between two walking fixes, 15 minutes each way
	pts := []TrackPoint{at(0, 51.500, -0.120), at(15, 51.501, -0.121), at(30, 51.502, -0.122),
		at(45, 52.040, -0.122), at(60, 51.503, -0.123), at(75, 51.504, -0.124)}
	q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{})
	if len(q) != 1 || q[0].Kind != "glitch" {
		t.Fatalf("questions %+v", q)
	}
	if q[0].AwayM < 55000 || q[0].GoneS != 30*60 || len(q[0].TS) != 1 || q[0].TS[0] != at(45, 0, 0).TS {
		t.Fatalf("the glitch: %+v", q[0])
	}
}

func TestAFastOutAndBackIsAskedAbout(t *testing.T) {
	// driving (not slow either side), and one fix 60 km off, 15 minutes out and back: 240 km/h
	pts := []TrackPoint{at(0, 51.00, -0.10), at(15, 51.10, -0.10), at(30, 51.20, -0.10),
		at(45, 51.74, -0.10), at(60, 51.25, -0.10), at(75, 51.35, -0.10)}
	q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{})
	if len(q) != 1 || q[0].Kind != "fast" || q[0].KMH < 200 {
		t.Fatalf("questions %+v", q)
	}
}

func TestADayTripIsNotAskedAbout(t *testing.T) {
	// out 30 km at motorway speed, two hours there, back: a real trip
	pts := []TrackPoint{at(0, 51.50, -0.12), at(15, 51.55, -0.12), at(30, 51.77, -0.12),
		at(60, 51.771, -0.121), at(90, 51.772, -0.12), at(120, 51.771, -0.12), at(150, 51.77, -0.12),
		at(165, 51.60, -0.12), at(180, 51.50, -0.12)}
	if q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{}); len(q) != 0 {
		t.Fatalf("a day trip asked about: %+v", q)
	}
}

func TestNoRoadThereIsAskedAbout(t *testing.T) {
	// on an island, then one fix 8 km out to sea at ferry pace, then back on the island
	pts := []TrackPoint{at(0, 39.20, 20.18), at(25, 39.201, 20.181), at(50, 39.27, 20.18),
		at(75, 39.202, 20.182), at(100, 39.203, 20.183)}
	sea := func(lat, lon, within float64) (bool, bool) { return lat < 39.25, true } // roads only on the island
	q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{NearRoad: sea})
	if len(q) != 1 || q[0].Kind != "offroad" {
		t.Fatalf("questions %+v", q)
	}
	// where the box cannot tell (no tiles), it does not ask
	unknown := func(lat, lon, within float64) (bool, bool) { return false, false }
	if q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{NearRoad: unknown}); len(q) != 0 {
		t.Fatalf("asked without knowing the roads: %+v", q)
	}
}

func TestAYesIsNotAskedAgain(t *testing.T) {
	pts := []TrackPoint{at(0, 51.00, -0.10), at(15, 51.10, -0.10), at(30, 51.20, -0.10),
		at(45, 51.74, -0.10), at(60, 51.25, -0.10), at(75, 51.35, -0.10)}
	kept := func(from, to int64) bool { return from <= at(45, 0, 0).TS && to >= at(45, 0, 0).TS }
	if q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{Kept: kept}); len(q) != 0 {
		t.Fatalf("asked again: %+v", q)
	}
}

func TestQuestionsRideOnTheDayPath(t *testing.T) {
	pts := []TrackPoint{at(0, 51.00, -0.10), at(15, 51.10, -0.10)}
	q := []Question{{From: 1, To: 2, TS: []int64{1, 2}, Kind: "fast", Place: "Bedford, United Kingdom"}}
	doc, err := BuildDayPathAsking(time.Unix(1790000000, 0), pts, nil, q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), `"questions"`) || !strings.Contains(string(doc), "Bedford") {
		t.Fatalf("no questions in %s", doc)
	}
	var none map[string]any
	doc2, _ := BuildDayPath(time.Unix(1790000000, 0), pts, nil)
	json.Unmarshal(doc2, &none)
	if strings.Contains(string(doc2), "questions") {
		t.Fatal("questions on a day with none")
	}
}

// Paxos and Corfu: land south of 39.25 and north of 39.36, the strait between them
var (
	lakka   = [2]float64{39.237, 20.132}
	kavos   = [2]float64{39.386, 20.115}
	islands = func(lat, lon float64) (bool, bool) { return lat < 39.25 || lat > 39.36, true }
)

// fixes at Lakka every 15 minutes from minute m0 to m1, wandering a few metres
func stayAt(p [2]float64, m0, m1 int) []TrackPoint {
	var out []TrackPoint
	for m := m0; m <= m1; m += 15 {
		out = append(out, at(m, p[0]+float64(m%45)*1e-5, p[1]))
	}
	return out
}

func TestASeaCrossingIsAskedAbout(t *testing.T) {
	// the Corfu line: a morning at Lakka, then the phone on a Kavos tower for an hour (four fixes,
	// so not a parked tower to the glitch rules), then Lakka again, 15 minutes a hop: 66 km/h, no road
	pts := stayAt(lakka, 0, 90)
	for _, m := range []int{105, 120, 135, 150} {
		pts = append(pts, at(m, kavos[0]+float64(m-105)*2e-4, kavos[1]))
	}
	pts = append(pts, stayAt(lakka, 165, 240)...)
	q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{OnLand: islands})
	if len(q) != 1 || q[0].Kind != "sea" || len(q[0].TS) != 4 {
		t.Fatalf("questions %+v", q)
	}
	if q[0].SeaM < 10000 || q[0].AwayM < 15000 || q[0].From != at(105, 0, 0).TS || q[0].GoneS != 75*60 {
		t.Fatalf("the crossing: %+v", q[0])
	}
	// the same run without land tiles is not asked (too slow for fast, too long for offroad)
	if q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{}); len(q) != 0 {
		t.Fatalf("asked without the land tiles: %+v", q)
	}
}

func TestASlowBoatIsNotAskedAbout(t *testing.T) {
	// a boat to Kavos (45 minutes, 22 km/h), half an hour there, an hour back: a real outing
	pts := stayAt(lakka, 0, 15)
	pts = append(pts, at(60, kavos[0], kavos[1]), at(75, kavos[0]+1e-4, kavos[1]), at(90, kavos[0], kavos[1]+1e-4))
	pts = append(pts, stayAt(lakka, 150, 210)...)
	if q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{OnLand: islands}); len(q) != 0 {
		t.Fatalf("a boat trip asked about: %+v", q)
	}
}

func TestAFlipBetweenTowersIsOneQuestion(t *testing.T) {
	// Lakka, Kavos, Lakka, Kavos over an afternoon: one question with both Kavos fixes
	pts := stayAt(lakka, 0, 45)
	pts = append(pts, at(60, kavos[0], kavos[1]), at(61, kavos[0]+1e-4, kavos[1]), at(62, kavos[0], kavos[1]+1e-4), at(63, kavos[0]+2e-4, kavos[1]))
	pts = append(pts, stayAt(lakka, 75, 120)...)
	pts = append(pts, at(135, kavos[0], kavos[1]), at(136, kavos[0]+1e-4, kavos[1]), at(137, kavos[0], kavos[1]+1e-4), at(138, kavos[0]+2e-4, kavos[1]))
	pts = append(pts, stayAt(lakka, 150, 210)...)
	q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{OnLand: islands})
	if len(q) != 1 || q[0].Kind != "sea" || len(q[0].TS) != 8 {
		t.Fatalf("questions %+v", q)
	}
	if q[0].From != at(60, 0, 0).TS || q[0].To != at(138, 0, 0).TS || q[0].GoneS != 105*60 {
		t.Fatalf("the merged question: %+v", q[0])
	}
}

func TestAJumpAtTheEdgeOfTheDataIsAskedAbout(t *testing.T) {
	// the data starts on a Kavos tower, then a morning at Lakka
	lead := append([]TrackPoint{at(0, kavos[0], kavos[1])}, stayAt(lakka, 15, 120)...)
	q := TrailQuestions(lead, dayStart, dayEnd, AskChecks{OnLand: islands})
	if len(q) != 1 || q[0].Kind != "sea" || q[0].From != at(0, 0, 0).TS || q[0].GoneS != 0 {
		t.Fatalf("leading: %+v", q)
	}
	// a morning at Lakka, and the last fix of the data on the Kavos tower
	trail := append(stayAt(lakka, 0, 105), at(120, kavos[0], kavos[1]))
	q = TrailQuestions(trail, dayStart, dayEnd, AskChecks{OnLand: islands})
	if len(q) != 1 || q[0].Kind != "sea" || q[0].From != at(120, 0, 0).TS {
		t.Fatalf("trailing: %+v", q)
	}
	// a short drive at the start of the data is not a jump: the run is the long side
	drive := []TrackPoint{at(0, 51.50, -0.12), at(15, 51.55, -0.12), at(30, 51.60, -0.12), at(45, 51.601, -0.12)}
	if q := TrailQuestions(drive, dayStart, dayEnd, AskChecks{}); len(q) != 0 {
		t.Fatalf("a drive at the edge asked about: %+v", q)
	}
}

func TestAHundredKilometresAnHourOutAndBackIsAskedAbout(t *testing.T) {
	// driving south at 30 km/h, then a fix 25 km north, 15 minutes each way: 100 km/h out and back
	pts := []TrackPoint{at(0, 51.50, -0.12), at(15, 51.43, -0.12), at(30, 51.36, -0.12),
		at(45, 51.585, -0.12), at(60, 51.35, -0.12), at(75, 51.28, -0.12)}
	q := TrailQuestions(pts, dayStart, dayEnd, AskChecks{})
	if len(q) != 1 || q[0].Kind != "fast" || q[0].KMH < 95 || q[0].KMH > 110 {
		t.Fatalf("questions %+v", q)
	}
}

func TestWaterRunM(t *testing.T) {
	a := TrackPoint{Lat: lakka[0], Lon: lakka[1]}
	b := TrackPoint{Lat: kavos[0], Lon: kavos[1]}
	w, known := WaterRunM(a, b, islands)
	if !known || w < 11000 || w > 13500 {
		t.Fatalf("water %v known %v", w, known)
	}
	if _, known := WaterRunM(a, b, func(float64, float64) (bool, bool) { return false, false }); known {
		t.Fatal("known without tiles")
	}
}
