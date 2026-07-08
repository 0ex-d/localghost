package search

// Fusion, grouping, and ranking (spec 7.4, 7.5, 8). RRF over the legs, group under the parent, then
// multiplicative adjustments. Anchor boost and cluster warmth read through interfaces that today have
// only a Zero implementation , the memory/cluster tables belong to the consolidation daemon, which
// does not exist yet. Running-but-blind: the ranking pipeline is real, the T2-dependent signals
// honestly contribute nothing until their producer ships. Invariant I3: nothing here reads clicks.

import (
	"math"
	"sort"
	"strings"
	"time"
)

type ChunkKey struct {
	Tier    int
	ChunkID int64
}

// Fuse is Reciprocal Rank Fusion, k=60 (spec 7.4).
func Fuse(legs ...[]Hit) map[ChunkKey]float64 {
	const k = 60.0
	s := map[ChunkKey]float64{}
	for _, leg := range legs {
		for i, h := range leg {
			s[ChunkKey{h.Tier, h.ChunkID}] += 1.0 / (k + float64(i+1))
		}
	}
	return s
}

// GroupKey identifies a result group (parent).
type GroupKey struct {
	Tier     int
	Source   string
	OrigID   int64
	EntryID  int64
	MemoryID int64
}

func groupOf(h Hit) GroupKey {
	return GroupKey{Tier: h.Tier, Source: h.OrigSource, OrigID: h.OrigID, EntryID: h.EntryID, MemoryID: h.MemoryID}
}

// Group is one ranked result group.
type Group struct {
	Key       GroupKey
	Score     float64
	TopChunks []Hit // best-first, capped at 3 for snippets
}

// GroupHits groups fused chunks under parents: docScore = max + 0.1*ln(1+extras) (spec 7.5).
func GroupHits(hits []Hit, fused map[ChunkKey]float64) []Group {
	byGroup := map[GroupKey][]Hit{}
	seen := map[ChunkKey]bool{}
	for _, h := range hits {
		k := ChunkKey{h.Tier, h.ChunkID}
		if seen[k] {
			continue
		}
		seen[k] = true
		h.Score = fused[k]
		byGroup[groupOf(h)] = append(byGroup[groupOf(h)], h)
	}
	out := make([]Group, 0, len(byGroup))
	for gk, hs := range byGroup {
		sort.Slice(hs, func(i, j int) bool { return hs[i].Score > hs[j].Score })
		score := hs[0].Score + 0.1*math.Log(1+float64(len(hs)-1))
		top := hs
		if len(top) > 3 {
			top = top[:3]
		}
		out = append(out, Group{Key: gk, Score: score, TopChunks: top})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// AnchorLookup and WarmthLookup are the T2-dependent ranking signals (spec 8.1, 8.2). The Zero
// implementation returns no boost; the consolidation daemon supplies a real one when it exists.
type AnchorLookup interface {
	// Anchored reports whether the memory is user-anchored, and whether its cluster contains an
	// anchored member at the given hop distance (0 = itself).
	Anchored(memoryID int64) (self bool, clusterHops int, clusterHas bool)
}

type WarmthLookup interface {
	// LastTouched returns when the memory's CLUSTER was last touched (zero time = unknown).
	LastTouched(memoryID int64) time.Time
}

type ZeroRankSignals struct{}

func (ZeroRankSignals) Anchored(int64) (bool, int, bool)  { return false, 0, false }
func (ZeroRankSignals) LastTouched(int64) time.Time       { return time.Time{} }

// Adjust applies the multiplicative ranking adjustments (spec 8) in place, then re-sorts.
func Adjust(groups []Group, query string, anchors AnchorLookup, warmth WarmthLookup, now time.Time) {
	specific, vague := queryShape(query)
	for i := range groups {
		g := &groups[i]
		// 8.1 anchor boost (T2 groups only; no-op under ZeroRankSignals)
		if g.Key.Tier == 2 && g.Key.MemoryID != 0 {
			self, hops, has := anchors.Anchored(g.Key.MemoryID)
			if self {
				g.Score *= 1.5
			} else if has {
				g.Score *= 1.0 + 0.3*math.Exp2(-float64(hops))
			}
			// 8.2 cluster warmth
			if lt := warmth.LastTouched(g.Key.MemoryID); !lt.IsZero() {
				ageDays := now.Sub(lt).Hours() / 24
				g.Score *= 1.0 + 0.25*math.Exp(-ageDays/90.0)
			}
		}
		// 8.3 tier prior by query shape
		if specific && g.Key.Tier == 0 {
			g.Score *= 1.2
		}
		if vague {
			switch g.Key.Tier {
			case 2:
				g.Score *= 1.3
			case 1:
				g.Score *= 1.15
			}
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Score > groups[j].Score })
}

// queryShape is the spec's two-line heuristic (8.3): quotes/identifier/>6 tokens = specific;
// <=3 tokens and no quotes = vague.
func queryShape(q string) (specific, vague bool) {
	tokens := strings.Fields(q)
	quoted := strings.Contains(q, `"`)
	ident := strings.ContainsAny(q, "_/") || strings.Contains(q, "://")
	specific = quoted || ident || len(tokens) > 6
	vague = len(tokens) <= 3 && !quoted
	return
}
