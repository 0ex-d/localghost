// Package synthd is the engine of the memory-surfacing daemon. It owns the retrieval side of the
// "Before You Ask" loop: given a context (what the user is doing now), it ranks memories from the
// index and returns candidates for ghost.cued to gate. cued decides WHEN and WHETHER; synthd decides
// WHAT is even a candidate.
//
// HONEST STATE. The INDEX , the embeddings, the vector store, the memory corpus , is the next few
// months of work and does not exist. So this engine ships the real query PIPELINE (intake, the
// ranking frame, the candidate shaping) over an EMPTY index: Query runs the whole path and returns
// nothing, because there is nothing indexed. synthd is running-but-blind, exactly like cued one layer
// up. When the index is built behind the Index interface, synthd starts returning candidates with no
// change to this pipeline or to cued.
package synthd

import (
	"context"
	"log/slog"
	"sort"

	"github.com/LocalGhostDao/localghost/server/internal/synth"
)

// Index is the retrieval backend synthd ranks over. A real implementation holds the embedded memory
// corpus and does nearest-neighbour lookup; the stub holds nothing. Kept an interface so the corpus
// work slots in behind it without touching the query pipeline.
type Index interface {
	// Lookup returns raw matches for a context query: memories the index thinks are near this moment,
	// with a similarity score. synthd turns these into ranked Candidates. Empty until the index exists.
	Lookup(ctx context.Context, q synth.Query) ([]Match, error)
	// Ready reports whether the corpus is actually loaded and queryable.
	Ready() bool
	// Size is the number of indexed memories, for the index-stats command.
	Size() int
}

// Match is one raw hit from the index before synthd ranks/shapes it.
type Match struct {
	ID         string
	Title      string
	Body       string
	Similarity float64  // raw index similarity to the query context, 0..1
	Options    []string // if this memory is an answerable ask
}

// Engine runs the query pipeline over an Index.
type Engine struct {
	index Index
	log   *slog.Logger
}

// New builds the engine over an index (the stub, until the corpus exists).
func New(index Index, log *slog.Logger) *Engine {
	return &Engine{index: index, log: log}
}

// Ready surfaces whether retrieval is actually available (drives cued's Ready() and synthd health).
func (e *Engine) Ready() bool { return e.index.Ready() }

// Size is the indexed-memory count for index-stats.
func (e *Engine) Size() int { return e.index.Size() }

// Query is the pipeline cued's Prime call runs: look up matches, shape them into ranked Candidates,
// return the top Limit. Runs end to end today; returns nothing because the index is empty.
func (e *Engine) Query(ctx context.Context, q synth.Query) ([]synth.Candidate, error) {
	matches, err := e.index.Lookup(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil // the normal case until the corpus exists
	}

	cands := make([]synth.Candidate, 0, len(matches))
	for _, m := range matches {
		cands = append(cands, synth.Candidate{
			ID:    m.ID,
			Title: m.Title,
			Body:  m.Body,
			// Relevance is the index similarity , how near this memory is to the current context.
			Relevance: m.Similarity,
			// Distinctiveness is how much the memory stands out from the moment. Without the corpus and
			// its base-rate statistics we cannot compute this properly yet, so the pipeline carries the
			// similarity through as a placeholder; the real distinctiveness (novelty vs the context's
			// expected memories) lands with the index. cued's threshold gates on both, so an honest
			// placeholder here is safe , it does not manufacture a false "stands out" signal.
			Distinctiveness: m.Similarity,
			Options:         m.Options,
		})
	}
	// Rank by relevance, highest first, and cap at the query limit.
	sort.Slice(cands, func(i, j int) bool { return cands[i].Relevance > cands[j].Relevance })
	if q.Limit > 0 && len(cands) > q.Limit {
		cands = cands[:q.Limit]
	}
	return cands, nil
}
