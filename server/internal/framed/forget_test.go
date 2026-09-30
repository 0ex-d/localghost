package framed

import (
	"reflect"
	"testing"
)

func TestSpotRunTakesTheTowersRepeats(t *testing.T) {
	// Lakka, then three fixes on the Kavos tower (a few metres apart), then Lakka again
	pts := []TrackPoint{at(0, 39.237, 20.132), at(15, 39.2371, 20.132),
		at(30, 39.386, 20.115), at(45, 39.3862, 20.1151), at(60, 39.3859, 20.115),
		at(75, 39.237, 20.132)}
	want := []int64{at(30, 0, 0).TS, at(45, 0, 0).TS, at(60, 0, 0).TS}
	for _, tap := range []int{30, 45, 60} {
		if got := SpotRun(pts, at(tap, 0, 0).TS, 150); !reflect.DeepEqual(got, want) {
			t.Fatalf("tap at %d: %v", tap, got)
		}
	}
	// a fix on its own is just itself
	if got := SpotRun(pts, at(75, 0, 0).TS, 150); len(got) != 1 {
		t.Fatalf("the last fix: %v", got)
	}
	// a second nobody recorded
	if got := SpotRun(pts, at(31, 0, 0).TS, 150); got != nil {
		t.Fatalf("no fix there: %v", got)
	}
	// the run stops at the first fix elsewhere, even when a later one is back at the spot
	pts = append(pts, at(90, 39.386, 20.115))
	if got := SpotRun(pts, at(90, 0, 0).TS, 150); len(got) != 1 {
		t.Fatalf("the return visit: %v", got)
	}
}
