package search

// The two entry points (spec 1): interactive Search and Ambient retrieval. Both run over the same
// store; ambient is leg B only, tiers 1+2, per spec 11.2. Legs run concurrently, each on its own
// connection (the API holds two clients precisely so leg A and leg B do not serialise on one mutex).

import (
	"context"
	"log/slog"
	"time"
)

type API struct {
	StoreA  *Store // leg A + assembly (one connection)
	StoreB  *Store // leg B (its own connection, so the legs truly run concurrently)
	Embed   *Embedder
	Anchors AnchorLookup
	Warmth  WarmthLookup
	Log     *slog.Logger

	EfSearch int // hnsw.ef_search per query (spec 13.2, default 80)
}

// Result is one assembled result group.
type Result struct {
	Tier       int      `json:"tier"`
	Label      string   `json:"label"`
	Path       string   `json:"path,omitempty"`
	CapturedAt int64    `json:"capturedAt,omitempty"`
	Score      float64  `json:"score"`
	Snippets   []string `json:"snippets,omitempty"`
	OrigSource string   `json:"origSource,omitempty"`
	OrigID     int64    `json:"origId,omitempty"`
	EntryID    int64    `json:"entryId,omitempty"`
	MemoryID   int64    `json:"memoryId,omitempty"`
}

// Search runs the full pipeline (spec 7): legs concurrent, RRF, group, adjust, assemble.
func (a *API) Search(ctx context.Context, query string, f Filters, limit int) ([]Result, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	type legOut struct {
		hits []Hit
		err  error
		name string
	}
	ch := make(chan legOut, 2)
	go func() {
		hits, err := a.StoreA.LegA(query, f, 100)
		ch <- legOut{hits, err, "A"}
	}()
	legs := 1
	if a.Embed != nil && a.StoreB != nil {
		legs = 2
		go func() {
			vecs, err := a.Embed.Embed(ctx, []string{query})
			if err != nil {
				ch <- legOut{nil, err, "B"}
				return
			}
			hits, err := a.StoreB.LegB(vecs[0], f, 100, a.ef(), a.Embed.ModelID)
			ch <- legOut{hits, err, "B"}
		}()
	}
	var all [][]Hit
	var flat []Hit
	for i := 0; i < legs; i++ {
		out := <-ch
		if out.err != nil {
			// One leg failing degrades to the other (FTS-only is the documented degraded mode). Log,
			// continue; fail only if BOTH legs return nothing usable.
			a.Log.Warn("search leg failed", "fn", "Search", "leg", out.name, "err", out.err)
			continue
		}
		all = append(all, out.hits)
		flat = append(flat, out.hits...)
	}
	if len(all) == 0 {
		return nil, context.Cause(ctx)
	}
	fused := Fuse(all...)
	groups := GroupHits(flat, fused)
	Adjust(groups, query, a.Anchors, a.Warmth, time.Now())
	if len(groups) > limit {
		groups = groups[:limit]
	}
	return a.assemble(query, groups), nil
}

func (a *API) ef() int {
	if a.EfSearch > 0 {
		return a.EfSearch
	}
	return 80
}

func (a *API) assemble(query string, groups []Group) []Result {
	var ids []int64
	for _, g := range groups {
		for _, h := range g.TopChunks {
			ids = append(ids, h.ChunkID)
		}
	}
	snips, err := a.StoreA.Snippets(query, ids)
	if err != nil {
		a.Log.Warn("snippets failed", "fn", "assemble", "err", err)
		snips = map[int64]string{}
	}
	out := make([]Result, 0, len(groups))
	for _, g := range groups {
		label, path, captured := a.StoreA.ParentLabel(g.TopChunks[0])
		r := Result{
			Tier: g.Key.Tier, Label: label, Path: path, CapturedAt: captured, Score: g.Score,
			OrigSource: g.Key.Source, OrigID: g.Key.OrigID, EntryID: g.Key.EntryID, MemoryID: g.Key.MemoryID,
		}
		for _, h := range g.TopChunks {
			if s, ok := snips[h.ChunkID]; ok && s != "" {
				r.Snippets = append(r.Snippets, s)
			}
		}
		out = append(out, r)
	}
	return out
}

// Ambient runs the ambient query (spec 11.2): vector only, tiers 1+2, no FTS. The caller (cued via
// the prime command) applies its own gate; searchd only retrieves and scores.
func (a *API) Ambient(ctx context.Context, synthetic string, limit int) ([]Hit, error) {
	if a.Embed == nil || a.StoreB == nil {
		return nil, nil // vector-less box: ambient has nothing to say, silence is the contract
	}
	vecs, err := a.Embed.Embed(ctx, []string{synthetic})
	if err != nil {
		return nil, err
	}
	return a.StoreB.LegB(vecs[0], Filters{Tiers: []int{1, 2}}, limit, a.ef(), a.Embed.ModelID)
}
