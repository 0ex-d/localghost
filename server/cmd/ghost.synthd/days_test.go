package main

import (
	"strings"
	"testing"
)

func fullDay() *dayFacts {
	return &dayFacts{Day: "2026-09-25", Weekday: "Friday", Photos: 14, Described: 9,
		Covers: []string{"a1", "b2", "c3"}, Places: []string{"Gaios", "Voutoumi"}, Country: "Greece",
		Tags:     []string{"beach", "boat", "sea", "taverna", "dog", "sunset"},
		Captions: []string{"A small harbour with fishing boats tied along a stone quay under a clear sky."},
		Line:     "near Strada A → Corner Café → Voutoumi · 1.4 km on foot, 10 km by road",
		Stays: []dayStayFact{{Name: "Strada A", Kind: "near", From: 1000, To: 4600}, {Name: "Corner Café", Kind: "cafe", From: 5000, To: 6800, Photos: 1},
			{Name: "Voutoumi", Kind: "beach", From: 8000, To: 9000}},
		Moves:  []dayMoveFact{{Mode: "walk", From: 4600, To: 5000, Meters: 1294}, {Mode: "ride", From: 6800, To: 8000, Meters: 9419}},
		WalkM:  1294, RideM: 9419, Fixes: 46, Points: 49,
		Steps: 8400, SleepMin: 430, ExerciseMin: 35, Feeling: "tired but happy",
		Notes: []string{"Boat for Saturday"}, Chats: []string{"ferry times to Corfu"},
		Outing: "Antipaxos · 19–23 Sep 2026", OutingDay: 3, OutingDays: 5, OutingAway: true}
}

func TestDayTemplateTitleSheetAndSignature(t *testing.T) {
	f := fullDay()
	if !f.hasSignal() || f.empty() {
		t.Fatal("signal")
	}
	if got := dayTitle(f); got != "Friday 25 September 2026 · Corner Café, Voutoumi" {
		t.Fatalf("title %q", got)
	}
	tpl := dayTemplate(f)
	for _, must := range []string{
		"Day 3 of 5 of Antipaxos · 19–23 Sep 2026.",
		"Near Strada A → Corner Café → Voutoumi · 1.4 km on foot, 10 km by road.",
		"14 photos around Gaios and Voutoumi, mostly beach, boat, sea, taverna and dog.",
		"8,400 steps, 35 min of exercise, 7h 10m of sleep.",
		"You said you felt tired but happy.",
		"You wrote: \"Boat for Saturday\".",
		"You asked the box about ferry times to Corfu.",
	} {
		if !strings.Contains(tpl, must) {
			t.Fatalf("template lacks %q:\n%s", must, tpl)
		}
	}
	sheet := strings.Join(f.sheet(), "\n")
	for _, must := range []string{
		"Day: Friday 25 September 2026",
		"Part of a longer outing: day 3 of 5 of \"Antipaxos · 19–23 Sep 2026\"", "Away from home",
		"Stayed 01:23 UTC to 01:53 UTC: Corner Café (cafe) (took a photo there)",
		"near Strada A", "Voutoumi (beach)",
		"walked about 1.3 km", "travelled by road or water about 9.4 km",
		"On foot in all: about 1.3 km", "By road or water in all: about 9.4 km",
		"Photos taken: 14", "Photographed at: Gaios, Voutoumi", "Country: Greece",
		"Seen in the photos (most often first): beach, boat, sea, taverna, dog, sunset",
		"A photo shows: A small harbour",
		"Steps counted by the phone: 8400", "Exercise recorded: 35 minutes", "Sleep recorded: 7 hours 10 minutes",
		"you said you felt: tired but happy", "You wrote a note titled: Boat for Saturday", "You started a chat with the box about: ferry times to Corfu",
	} {
		if !strings.Contains(sheet, must) {
			t.Fatalf("sheet lacks %q:\n%s", must, sheet)
		}
	}
	// the model's memory over this sheet passes when it keeps to it
	good := "Day 3 of your 5 days around Antipaxos began near Strada A and moved on to the Corner Café, where you took a photo before the ride out to Voutoumi, 9.4 km by road. You walked about 1.3 km in all and the phone counted 8,400 steps. The photos are mostly beach, boats and sea, with a taverna and a dog among them; in the evening you said you felt tired but happy, and you left yourself a note about the boat for Saturday."
	if _, ok := groundedProse(good, f.sheet()); !ok {
		t.Fatal("a memory inside the sheet refused")
	}
	if _, ok := groundedProse("You spent day 3 of 5 around Antipaxos and walked 12 km before a swim at Voutoumi, then slept 8 hours.", f.sheet()); ok {
		t.Fatal("invented numbers accepted")
	}
	// the signature moves with the sheet and only with it
	sig := f.signature()
	g := fullDay()
	if g.signature() != sig {
		t.Fatal("same facts, different signature")
	}
	g.Described = 10
	if g.signature() == sig {
		t.Fatal("a caption landing must change the signature")
	}
	// a thin day: template only says what there is; no signal
	thin := &dayFacts{Day: "2026-01-04", Weekday: "Sunday", Steps: 2100, SleepMin: 60*8 + 5}
	if thin.hasSignal() || thin.empty() {
		t.Fatal("thin day")
	}
	if got := dayTemplate(thin); got != "2,100 steps, 8h 05m of sleep." {
		t.Fatalf("thin template %q", got)
	}
	if got := dayTitle(thin); got != "Sunday 4 January 2026" {
		t.Fatalf("thin title %q", got)
	}
	// a day with photos but no stays takes its title from the photos' places; no route line, the
	// distances speak
	ph := &dayFacts{Day: "2025-07-15", Photos: 3, Places: []string{"Lisbon"}, WalkM: 5200, RideM: 0}
	if got := dayTitle(ph); got != "Tuesday 15 July 2025 · Lisbon" {
		t.Fatalf("photo title %q", got)
	}
	if got := dayTemplate(ph); got != "5.2 km on foot. 3 photos around Lisbon." {
		t.Fatalf("photo template %q", got)
	}
	if (&dayFacts{Day: "2025-07-15"}).empty() != true {
		t.Fatal("empty")
	}
	if joinAnd([]string{"a"}) != "a" || joinAnd([]string{"a", "b"}) != "a and b" || joinAnd([]string{"a", "b", "c"}) != "a, b and c" || thousands(1234567) != "1,234,567" || thousands(999) != "999" {
		t.Fatal("small words")
	}
	if topKeys(map[string]int{"x": 1, "y": 3, "z": 3}, 2)[0] != "y" {
		t.Fatal("topKeys order")
	}
}
