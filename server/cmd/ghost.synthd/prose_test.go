package main

import (
	"strings"
	"testing"
)

func TestGroundedProseKeepsOnlyWhatStaysInsideTheFacts(t *testing.T) {
	facts := []string{
		"Title: Antipaxos · 19–23 Sep 2026",
		"Days: 5", "Photos taken: 30", "Main place: Antipaxos", "Country: Greece",
		"Moved about 27 km over the days (on foot or by vehicle)",
		"A trip, about 2300 km from home",
		"Seen in the photos (most often first): beach, boat, sea, sunset, taverna",
		"Mon 21 Sep: Gaios → Voutoumi · 2.3 km on foot",
	}
	good := "You spent five days around Antipaxos, in Greece, 2300 km from home. Most of your photos were of the beach, the boats and the sea, with a sunset or two and an evening at a taverna. On the Monday you walked from Gaios down to Voutoumi, 2.3 km on foot, and over the days you covered about 27 km in all."
	out, ok := groundedProse(good, facts)
	if !ok || !strings.HasPrefix(out, "You spent five days") {
		t.Fatalf("good prose refused: %v %q", ok, out)
	}
	// a number the facts do not have
	if _, ok := groundedProse("You spent 5 days there and swam 12 times.", facts); ok {
		t.Fatal("an invented number kept")
	}
	// a decimal's integer part is fine, another decimal is not
	if _, ok := groundedProse("You walked about 2 km from Gaios to Voutoumi on the Monday, and the sea was everywhere you looked that week.", facts); !ok {
		t.Fatal("the integer part of 2.3 refused")
	}
	if _, ok := groundedProse("You walked about 2.5 km from Gaios to Voutoumi on the Monday, and the sea was everywhere you looked that week.", facts); ok {
		t.Fatal("2.5 accepted against 2.3")
	}
	// lists, headings, refusals, dressing
	for _, bad := range []string{
		"- You went to Antipaxos\n- You saw the beach and the sea and the boats all week long",
		"## A memory\nYou went to Antipaxos for five days and photographed the beach and the sea.",
		"I'm sorry, but I cannot write a memory from these facts as they are personal and I have no context.",
		"**You** went to Antipaxos for five days and photographed the beach and the sea and the boats.",
		"Short.",
		"Here is the memory you asked for, based on the facts provided above about your trip.",
	} {
		if _, ok := groundedProse(bad, facts); ok {
			t.Fatalf("kept: %q", bad)
		}
	}
	// a preamble line the model adds is cut; exclamation marks become full stops; newlines fold
	out, ok = groundedProse("Your memory:\n\nYou spent five days around Antipaxos, in Greece!\nThe beach and the boats filled most of your photos, and one Monday you walked from Gaios to Voutoumi.", facts)
	if !ok || strings.Contains(out, "!") || strings.Contains(out, "\n") || strings.HasPrefix(out, "Your memory") {
		t.Fatalf("cleanup: %v %q", ok, out)
	}
	// quotes around the whole thing are stripped
	if out, ok := groundedProse("\"You spent five days around Antipaxos, in Greece, photographing the beach, the boats and the sea.\"", facts); !ok || strings.HasPrefix(out, "\"") {
		t.Fatalf("quotes: %v %q", ok, out)
	}
}

func TestMemoryPrompt(t *testing.T) {
	p := memoryPrompt([]string{"Day: Monday", "Stayed 08:00 UTC to 09:00 UTC: Corner Café (cafe)"}, "a day")
	for _, must := range []string{"FACTS:", "- Day: Monday", "second person", "No headings", "Reply with the memory only"} {
		if !strings.Contains(p, must) {
			t.Fatalf("prompt lacks %q", must)
		}
	}
	if kmText(9419) != "9.4 km" || kmText(27300) != "27 km" || kmText(950) != "1 km" {
		t.Fatalf("km: %s %s %s", kmText(9419), kmText(27300), kmText(950))
	}
	// thousands separators in the model's numbers are read as the numbers they are
	facts := []string{"Steps counted by the phone: 8400", "On foot in all: about 6.2 km"}
	if _, ok := groundedProse("You walked 6.2 km that day, 8,400 steps by the phone's count, most of them before lunch.", facts); !ok {
		t.Fatal("8,400 refused against 8400")
	}
	if _, ok := groundedProse("You walked 6.2 km that day, 9,400 steps by the phone's count, most of them before lunch.", facts); ok {
		t.Fatal("9,400 accepted")
	}
}
