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
	q := TrailQuestions(pts, dayStart, dayEnd, nil, nil)
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
	q := TrailQuestions(pts, dayStart, dayEnd, nil, nil)
	if len(q) != 1 || q[0].Kind != "fast" || q[0].KMH < 200 {
		t.Fatalf("questions %+v", q)
	}
}

func TestADayTripIsNotAskedAbout(t *testing.T) {
	// out 30 km at motorway speed, two hours there, back: a real trip
	pts := []TrackPoint{at(0, 51.50, -0.12), at(15, 51.55, -0.12), at(30, 51.77, -0.12),
		at(60, 51.771, -0.121), at(90, 51.772, -0.12), at(120, 51.771, -0.12), at(150, 51.77, -0.12),
		at(165, 51.60, -0.12), at(180, 51.50, -0.12)}
	if q := TrailQuestions(pts, dayStart, dayEnd, nil, nil); len(q) != 0 {
		t.Fatalf("a day trip asked about: %+v", q)
	}
}

func TestNoRoadThereIsAskedAbout(t *testing.T) {
	// on an island, then one fix 8 km out to sea at ferry pace, then back on the island
	pts := []TrackPoint{at(0, 39.20, 20.18), at(25, 39.201, 20.181), at(50, 39.27, 20.18),
		at(75, 39.202, 20.182), at(100, 39.203, 20.183)}
	sea := func(lat, lon, within float64) (bool, bool) { return lat < 39.25, true } // roads only on the island
	q := TrailQuestions(pts, dayStart, dayEnd, sea, nil)
	if len(q) != 1 || q[0].Kind != "offroad" {
		t.Fatalf("questions %+v", q)
	}
	// where the box cannot tell (no tiles), it does not ask
	unknown := func(lat, lon, within float64) (bool, bool) { return false, false }
	if q := TrailQuestions(pts, dayStart, dayEnd, unknown, nil); len(q) != 0 {
		t.Fatalf("asked without knowing the roads: %+v", q)
	}
}

func TestAYesIsNotAskedAgain(t *testing.T) {
	pts := []TrackPoint{at(0, 51.00, -0.10), at(15, 51.10, -0.10), at(30, 51.20, -0.10),
		at(45, 51.74, -0.10), at(60, 51.25, -0.10), at(75, 51.35, -0.10)}
	kept := func(from, to int64) bool { return from <= at(45, 0, 0).TS && to >= at(45, 0, 0).TS }
	if q := TrailQuestions(pts, dayStart, dayEnd, nil, kept); len(q) != 0 {
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
