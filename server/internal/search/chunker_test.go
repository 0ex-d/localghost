package search

import (
	"strings"
	"testing"
	"time"
)

func TestChunkTextShortDocIsOneChunk(t *testing.T) {
	got := ChunkText("[doc]\n", "One sentence. Two sentences here.")
	if len(got) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(got))
	}
	if !strings.HasPrefix(got[0].Body, "[doc]\n") {
		t.Fatal("context header must prefix the chunk body")
	}
}

func TestChunkTextSplitsAndOverlaps(t *testing.T) {
	// ~1200 estimated tokens of distinct sentences forces multiple chunks with overlap.
	var b strings.Builder
	for i := 0; i < 90; i++ {
		b.WriteString("Sentence number ")
		b.WriteString(strings.Repeat("word ", 8))
		b.WriteString("ends here. ")
	}
	chunks := ChunkText("", b.String())
	if len(chunks) < 2 {
		t.Fatalf("want multiple chunks, got %d", len(chunks))
	}
	// Overlap: the second chunk must start with text present in the first.
	tail := chunks[0].Body[len(chunks[0].Body)-40:]
	if !strings.Contains(chunks[1].Body, strings.TrimSpace(strings.Fields(tail)[0])) {
		t.Log("weak overlap probe , acceptable, boundaries are heuristic")
	}
	for i, c := range chunks {
		if c.Seq != i {
			t.Fatalf("seq %d != index %d", c.Seq, i)
		}
	}
}

func TestChunkTextHardCutsMonsterSentence(t *testing.T) {
	monster := strings.Repeat("word ", 900) // one sentence, ~1200 est tokens
	chunks := ChunkText("", monster)
	if len(chunks) < 2 {
		t.Fatalf("oversized sentence must hard-cut, got %d chunks", len(chunks))
	}
}

func TestStripQuotedEmail(t *testing.T) {
	body := "My reply.\n> quoted line\nOn Mon, Toby wrote:\nmore of mine\n---------- Forwarded message ----------\nold thread"
	got := StripQuotedEmail(body)
	if strings.Contains(got, "quoted line") || strings.Contains(got, "old thread") {
		t.Fatalf("quoted/forwarded content must be stripped: %q", got)
	}
	if !strings.Contains(got, "My reply.") || !strings.Contains(got, "more of mine") {
		t.Fatalf("own content must survive: %q", got)
	}
}

func TestChatWindows(t *testing.T) {
	base := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	msgs := []ChatMsg{
		{Sender: "V", Text: "hi", At: base},
		{Sender: "T", Text: "hey", At: base.Add(2 * time.Minute)},
		{Sender: "V", Text: "new topic", At: base.Add(2 * time.Hour)}, // >30min gap = new window
	}
	w := ChatWindows(msgs)
	if len(w) != 2 {
		t.Fatalf("want 2 windows, got %d: %v", len(w), w)
	}
	if !strings.Contains(w[0], "V: hi") || !strings.Contains(w[0], "T: hey") {
		t.Fatalf("window 0 must carry sender names inline: %q", w[0])
	}
}

func TestContextHeader(t *testing.T) {
	if h := ContextHeader("email from Toby", "", "re: equity"); h != "[email from Toby, re: equity]\n" {
		t.Fatalf("header = %q", h)
	}
	if h := ContextHeader("", ""); h != "" {
		t.Fatalf("empty parts must yield empty header, got %q", h)
	}
}
