package pair

import (
	"math/rand"
	"strings"
	"testing"
)

func TestChunkRoundTrip(t *testing.T) {
	// a realistic-size link (cert+key push it past 1KB) plus edge sizes
	big := "localghost://enroll?v=2&host=192.168.1.50&port=8443&fp=AB12CD34&cert=" +
		strings.Repeat("Q", 900) + "&key=" + strings.Repeat("Z", 340) + "&name=box"
	for _, link := range []string{"x", "localghost://enroll?host=10.0.0.1&fp=CD", big} {
		frames := ChunkLink(link)
		back, err := JoinFrames(frames)
		if err != nil {
			t.Fatalf("join failed for %d-byte link: %v", len(link), err)
		}
		if back != link {
			t.Fatalf("round-trip mismatch (%d bytes, %d frames)", len(link), len(frames))
		}
	}
}

func TestChunkOrderIndependentAndDupeSafe(t *testing.T) {
	link := "localghost://enroll?v=2&host=10.0.0.9&fp=AA&cert=" + strings.Repeat("C", 700)
	frames := ChunkLink(link)
	if len(frames) < 2 {
		t.Fatalf("expected multiple frames, got %d", len(frames))
	}
	shuffled := append([]string{}, frames...)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	shuffled = append(shuffled, frames[0], frames[len(frames)-1]) // duplicates
	back, err := JoinFrames(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if back != link {
		t.Fatal("shuffled+duped frames must still reassemble exactly")
	}
}

func TestChunkRejectsBadSets(t *testing.T) {
	link := "localghost://enroll?v=2&host=10.0.0.9&fp=AA&cert=" + strings.Repeat("C", 700)
	frames := ChunkLink(link)
	// missing frame
	if _, err := JoinFrames(frames[1:]); err == nil {
		t.Fatal("missing frame must fail")
	}
	// mixed sets: a frame from a different link
	other := ChunkLink("localghost://enroll?v=2&host=1.1.1.1&fp=BB&cert=" + strings.Repeat("D", 700))
	mixed := []string{frames[0], other[1]}
	if _, err := JoinFrames(mixed); err == nil {
		t.Fatal("frames from different enrolments must be rejected")
	}
}

func TestSingleFrameForSmallLink(t *testing.T) {
	frames := ChunkLink("localghost://enroll?host=10.0.0.1&fp=CD")
	if len(frames) != 1 {
		t.Fatalf("a small link should be one frame, got %d", len(frames))
	}
}
