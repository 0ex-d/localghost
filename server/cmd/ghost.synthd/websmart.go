package main

// THE SMARTER WEB SEARCH. The phone searches, the box never does; what changed is who decides
// what to look for and what to read.
//
//  1. THE NEED (/plan, before the phone searches): the model is asked, in one short call, what fact
//     would answer the question and which one to three searches would find it , so "and in
//     euros?" after a price question becomes the price in euros, not a search for "in euros".
//     The phone runs those searches; the box's own fallback is the phone's old keyword plan.
//  2. THE PARAGRAPHS: the phone sends each page's cleaned paragraphs, not one keyword window. The
//     box embeds the need and the paragraphs (searchd's EmbeddingGemma, over its socket) and
//     keeps, per page, the few that say what is needed , the passage that answers, not the one
//     that merely repeats the words.
//  3. ONE MORE ROUND: when nothing read comes close to the need and the plan had a search the
//     phone did not run yet, the chat stream says so ("more") instead of answering thin; the
//     phone runs it and asks again, once.
//
// Everything degrades: no model answer to the plan → the phone plans; no embedder → the keyword
// window the phone already cut; never a loop.

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
)

// webPlan is the model's statement of what the question needs from the web.
type webPlan struct {
	Search  bool     `json:"search"`
	Need    string   `json:"need"`
	Shape   string   `json:"shape"` // number | date | name | list | howto | prose
	Fresh   bool     `json:"fresh"`
	Queries []string `json:"queries"`
}

var planShapes = map[string]bool{"number": true, "date": true, "name": true, "list": true, "howto": true, "prose": true}

// planPrompt asks for the plan as JSON, with the last turns for what "it" and "there" mean.
func planPrompt(prompt string, history []chatTurn, now time.Time) string {
	var b strings.Builder
	b.WriteString("You plan a web search for a question asked in a chat. Today is " + now.UTC().Format("2006-01-02") + ".\n")
	if n := len(history); n > 0 {
		b.WriteString("The last turns, oldest first, so pronouns can be resolved:\n")
		start := n - 4
		if start < 0 {
			start = 0
		}
		for _, t := range history[start:] {
			b.WriteString(t.Role + ": " + clip(strings.ReplaceAll(t.Content, "\n", " "), 300) + "\n")
		}
	}
	b.WriteString("The question: " + clip(strings.ReplaceAll(prompt, "\n", " "), 600) + "\n\n")
	b.WriteString(`Reply with one JSON object and nothing else:
{"search": true or false, "need": "one sentence: the fact or content that would answer the question", "shape": "number|date|name|list|howto|prose", "fresh": true or false, "queries": ["search query", ...]}
search is false when the question needs no outside facts (small talk, the person's own photos, memories or trail, arithmetic, a request to write something). need names the specific thing, with the place, the currency, the year when they matter. fresh is true when the answer changes over time (prices, news, weather, who holds a job, the latest version). queries: one to three, most specific first, each a short search engine query, the way a person types one; the last may be a broader fallback.
The queries leave for a search engine, so they are about the outside world only. Never put the person's own details from the conversation in them: the names of people they know, where they live or were, their health, money, work or relationships. Ask for the public thing instead: "cafes open now in Loggos", not "the cafe where I met Maria".`)
	return b.String()
}

// parsePlan reads the model's answer, whatever it wrapped it in, and keeps only what is valid.
func parsePlan(out string) (webPlan, bool) {
	i := strings.Index(out, "{")
	j := strings.LastIndex(out, "}")
	if i < 0 || j <= i {
		return webPlan{}, false
	}
	var p webPlan
	if err := json.Unmarshal([]byte(out[i:j+1]), &p); err != nil {
		return webPlan{}, false
	}
	p.Need = clip(strings.TrimSpace(strings.ReplaceAll(p.Need, "\n", " ")), 300)
	if !planShapes[p.Shape] {
		p.Shape = "prose"
	}
	var qs []string
	seen := map[string]bool{}
	for _, q := range p.Queries {
		q = strings.TrimSpace(strings.ReplaceAll(q, "\n", " "))
		q = strings.Trim(q, "\"'“”")
		if q == "" || len(q) > 120 {
			continue
		}
		k := strings.ToLower(q)
		if seen[k] {
			continue
		}
		seen[k] = true
		qs = append(qs, q)
		if len(qs) == 3 {
			break
		}
	}
	p.Queries = qs
	if p.Search && (p.Need == "" || len(p.Queries) == 0) {
		return webPlan{}, false
	}
	return p, true
}

// --- the paragraphs ---

const (
	rankMaxParas   = 56   // paragraphs embedded per question (the need takes one more slot)
	rankPerPage    = 3    // paragraphs kept per page
	rankThinBelow  = 0.30 // best similarity under this: nothing read comes close to the need
	rankParaMaxLen = 1200
)

// embedVia asks searchd for unit vectors. nil when searchd or its embedder is not there.
func embedVia(runDir string, texts []string) [][]float32 {
	c := ctlsock.NewClientTimeout("ghost.searchd", runDir, 20*time.Second)
	resp, err := c.Call("embed", map[string]any{"texts": texts})
	if err != nil || !resp.OK {
		return nil
	}
	var out struct {
		Vectors [][]float32 `json:"vectors"`
	}
	if json.Unmarshal(resp.Data, &out) != nil || len(out.Vectors) != len(texts) {
		return nil
	}
	return out.Vectors
}

func dot(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// rankParagraphs gives each page hit an excerpt made of the paragraphs closest to the need, in
// the page's own order, and reports the best similarity seen (0 when nothing had paragraphs) and
// whether an embedder did the ranking (false: the keyword pick stood in).
func rankParagraphs(runDir, need string, hits []webHit) (best float64, embedded bool) {
	type ref struct{ hit, para int }
	var texts []string
	var refs []ref
	for i := range hits {
		for j, p := range hits[i].Paragraphs {
			if len(texts) >= rankMaxParas {
				break
			}
			p = clip(strings.TrimSpace(p), rankParaMaxLen)
			if len(p) < 40 {
				continue
			}
			hits[i].Paragraphs[j] = p
			texts = append(texts, p)
			refs = append(refs, ref{i, j})
		}
	}
	if len(texts) == 0 {
		return 0, false
	}
	vecs := embedVia(runDir, append([]string{need}, texts...))
	sims := make([]float64, len(texts))
	if vecs != nil {
		embedded = true
		for k := range texts {
			sims[k] = dot(vecs[0], vecs[k+1])
		}
	} else {
		terms := keyTerms(need)
		for k, t := range texts {
			sims[k] = termScore(t, terms)
		}
	}
	// per page: the top rankPerPage by similarity, then back into page order
	type scored struct {
		para int
		sim  float64
	}
	perHit := map[int][]scored{}
	for k, r := range refs {
		perHit[r.hit] = append(perHit[r.hit], scored{r.para, sims[k]})
		if sims[k] > best {
			best = sims[k]
		}
	}
	for i := range hits {
		ss := perHit[i]
		if len(ss) == 0 {
			continue
		}
		sort.SliceStable(ss, func(a, b int) bool { return ss[a].sim > ss[b].sim })
		if len(ss) > rankPerPage {
			ss = ss[:rankPerPage]
		}
		sort.Slice(ss, func(a, b int) bool { return ss[a].para < ss[b].para })
		var parts []string
		total := 0
		for _, s := range ss {
			p := hits[i].Paragraphs[s.para]
			if total+len(p) > webMaxExcerpt && len(parts) > 0 {
				break
			}
			parts = append(parts, p)
			total += len(p)
		}
		hits[i].Excerpt = clip(strings.Join(parts, " … "), webMaxExcerpt)
		hits[i].Paragraphs = nil // the prompt gets the excerpt; the rest was read and set aside
	}
	if !embedded {
		best = math.Min(best, 1)
	}
	return best, embedded
}

// keyTerms is the need's words worth matching, lower-cased, short and common ones dropped.
func keyTerms(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127)
	}) {
		if len(w) < 3 || stopWords[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

var stopWords = map[string]bool{"the": true, "and": true, "for": true, "that": true, "this": true, "with": true, "what": true, "which": true, "who": true, "when": true, "where": true, "how": true, "are": true, "was": true, "were": true, "does": true, "did": true, "has": true, "have": true, "its": true, "from": true, "into": true, "about": true, "one": true, "sentence": true, "fact": true, "answer": true, "question": true, "current": true, "would": true}

// termScore is the share of the need's terms a paragraph contains, 0..1.
func termScore(text string, terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	t := strings.ToLower(text)
	n := 0
	for _, w := range terms {
		if strings.Contains(t, w) {
			n++
		}
	}
	return float64(n) / float64(len(terms))
}

// planBox is the box's speed as the phone reads it: where the model runs and how fast it reads
// (measured prompt tokens per second, or the class default when nothing was measured yet), so
// the phone can weigh reading the pages itself against sending them.
func planBox(sp engineSpeed) map[string]any {
	return map[string]any{"known": sp.Known, "onGPU": sp.OnGPU, "promptTPS": math.Round(sp.promptTPS()), "genTPS": math.Round(sp.GenTPS)}
}
