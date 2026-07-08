// Package search is the engine of ghost.searchd: chunking, embedding, hybrid retrieval (FTS + vector
// with RRF fusion), ranking, the job queue workers, and the ambient query path ghost.cued consumes.
// SPEC v1.1 is the authority; deviations are commented where they occur, never silent.
package search

import (
	"fmt"
	"strings"
	"time"
)

// Chunking rules (spec 5): target 400 estimated tokens, 15% overlap, split priority paragraph >
// sentence > hard cut, max 2000 chunks per original. Token estimate is len(words)*4/3 , sizing only.

const (
	targetTokens   = 400
	overlapPct     = 15
	maxChunks      = 2000
)

// estTokens estimates token count from word count (spec 5).
func estTokens(words int) int { return words * 4 / 3 }

// Chunk is one produced chunk body with its sequence.
type Chunk struct {
	Seq  int
	Body string
}

// ChunkText splits text per the spec rules and prefixes every chunk with the context header (spec 5:
// the header is part of the stored body, shown in snippets, embedded and FTS-indexed).
func ChunkText(header, text string) []Chunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	paras := splitParagraphs(text)
	var (
		chunks  []Chunk
		current []string // sentences accumulated for the current chunk
		words   int
	)
	flush := func() {
		if len(current) == 0 {
			return
		}
		body := strings.TrimSpace(strings.Join(current, " "))
		if body == "" {
			current = nil
			words = 0
			return
		}
		chunks = append(chunks, Chunk{Seq: len(chunks), Body: header + body})
		// 15% overlap: carry the trailing sentences of this chunk into the next.
		keep := overlapSentences(current, words*overlapPct/100)
		current = keep
		words = wordCount(keep)
	}
	for _, para := range paras {
		sents := splitSentences(para)
		for _, sent := range sents {
			w := len(strings.Fields(sent))
			// A single sentence larger than the target gets hard-cut (split priority's last resort).
			if estTokens(w) > targetTokens {
				flush()
				for _, piece := range hardCut(sent, targetTokens) {
					chunks = append(chunks, Chunk{Seq: len(chunks), Body: header + piece})
					if len(chunks) >= maxChunks {
						return chunks
					}
				}
				current, words = nil, 0
				continue
			}
			if estTokens(words+w) > targetTokens {
				flush()
				if len(chunks) >= maxChunks {
					return chunks
				}
			}
			current = append(current, sent)
			words += w
		}
	}
	flush()
	if len(chunks) > maxChunks {
		chunks = chunks[:maxChunks] // spec: log and truncate; the caller logs
	}
	return chunks
}

// ContextHeader builds the one-line header from metadata (spec 5). Empty parts are skipped.
// Example: "[email from Toby, 2024-03-11, re: equity terms]\n"
func ContextHeader(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			keep = append(keep, strings.TrimSpace(p))
		}
	}
	if len(keep) == 0 {
		return ""
	}
	return "[" + strings.Join(keep, ", ") + "]\n"
}

// StripQuotedEmail removes quoted history before chunking (spec 5): '>' prefixed lines, "On ...
// wrote:" attribution lines, and forwarded banners. Quoted text is already indexed under its own
// original; re-indexing it makes five copies of every thread.
func StripQuotedEmail(body string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, ">") {
			continue
		}
		lower := strings.ToLower(t)
		if strings.HasPrefix(lower, "on ") && strings.HasSuffix(lower, "wrote:") {
			continue
		}
		if strings.HasPrefix(lower, "---------- forwarded message") ||
			strings.HasPrefix(lower, "-----original message-----") ||
			strings.HasPrefix(lower, "begin forwarded message") {
			break // everything after a forward banner is the forwarded copy
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// ChatWindow groups messages into conversation windows (spec 5): same thread, gap < 30 min. Each
// window becomes one chunkable body with sender names inline. A six-word message is never its own
// chunk.
type ChatMsg struct {
	Sender string
	Text   string
	At     time.Time
}

func ChatWindows(msgs []ChatMsg) []string {
	if len(msgs) == 0 {
		return nil
	}
	var windows []string
	var b strings.Builder
	last := msgs[0].At
	flush := func() {
		if b.Len() > 0 {
			windows = append(windows, strings.TrimSpace(b.String()))
			b.Reset()
		}
	}
	for _, m := range msgs {
		if m.At.Sub(last) > 30*time.Minute {
			flush()
		}
		fmt.Fprintf(&b, "%s: %s\n", m.Sender, m.Text)
		last = m.At
	}
	flush()
	return windows
}

// --- internals ---

func splitParagraphs(t string) []string {
	raw := strings.Split(t, "\n\n")
	var out []string
	for _, p := range raw {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return out
}

// splitSentences is deliberately simple: split after . ! ? followed by whitespace. Abbreviation
// misfires cost a slightly odd chunk boundary, not correctness , the overlap absorbs them.
func splitSentences(p string) []string {
	var out []string
	start := 0
	for i := 0; i < len(p)-1; i++ {
		if (p[i] == '.' || p[i] == '!' || p[i] == '?') && (p[i+1] == ' ' || p[i+1] == '\n' || p[i+1] == '\t') {
			out = append(out, strings.TrimSpace(p[start:i+1]))
			start = i + 1
		}
	}
	if rest := strings.TrimSpace(p[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

func wordCount(sents []string) int {
	n := 0
	for _, s := range sents {
		n += len(strings.Fields(s))
	}
	return n
}

// overlapSentences returns the trailing sentences totalling roughly wantWords.
func overlapSentences(sents []string, wantWords int) []string {
	if wantWords <= 0 {
		return nil
	}
	total := 0
	i := len(sents)
	for i > 0 && total < wantWords {
		i--
		total += len(strings.Fields(sents[i]))
	}
	out := make([]string, len(sents)-i)
	copy(out, sents[i:])
	return out
}

// hardCut splits an oversized sentence into word-boundary pieces of about targetTok tokens.
func hardCut(sent string, targetTok int) []string {
	words := strings.Fields(sent)
	perPiece := targetTok * 3 / 4 // invert the 4/3 estimate back to words
	if perPiece < 1 {
		perPiece = 1
	}
	var out []string
	for i := 0; i < len(words); i += perPiece {
		j := i + perPiece
		if j > len(words) {
			j = len(words)
		}
		out = append(out, strings.Join(words[i:j], " "))
	}
	return out
}
