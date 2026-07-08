package search

import (
	"testing"
	"time"
)

func TestFuseRRF(t *testing.T) {
	a := []Hit{{Tier: 0, ChunkID: 1}, {Tier: 0, ChunkID: 2}}
	b := []Hit{{Tier: 0, ChunkID: 2}, {Tier: 0, ChunkID: 3}}
	f := Fuse(a, b)
	// chunk 2 appears in both legs (ranks 2 and 1) and must beat both single-leg chunks.
	if !(f[ChunkKey{0, 2}] > f[ChunkKey{0, 1}] && f[ChunkKey{0, 2}] > f[ChunkKey{0, 3}]) {
		t.Fatalf("RRF must reward presence in both legs: %v", f)
	}
}

func TestGroupHitsAggregates(t *testing.T) {
	hits := []Hit{
		{Tier: 0, ChunkID: 1, OrigSource: "email", OrigID: 7},
		{Tier: 0, ChunkID: 2, OrigSource: "email", OrigID: 7},
		{Tier: 0, ChunkID: 3, OrigSource: "email", OrigID: 8},
	}
	fused := map[ChunkKey]float64{{0, 1}: 0.5, {0, 2}: 0.4, {0, 3}: 0.5}
	g := GroupHits(hits, fused)
	if len(g) != 2 {
		t.Fatalf("want 2 groups, got %d", len(g))
	}
	// doc 7 has max 0.5 plus a multi-chunk bonus; doc 8 has bare 0.5, so 7 must rank first.
	if g[0].Key.OrigID != 7 {
		t.Fatalf("multi-chunk doc must outrank equal-max single: %+v", g)
	}
}

func TestAdjustTierPriorsByQueryShape(t *testing.T) {
	mk := func() []Group {
		return []Group{
			{Key: GroupKey{Tier: 0}, Score: 1.0},
			{Key: GroupKey{Tier: 2, MemoryID: 5}, Score: 1.0},
		}
	}
	vague := mk()
	Adjust(vague, "toby equity", ZeroRankSignals{}, ZeroRankSignals{}, time.Now())
	if vague[0].Key.Tier != 2 {
		t.Fatalf("vague query must prefer T2: %+v", vague)
	}
	specific := mk()
	Adjust(specific, `"exact phrase" plus many more tokens here now`, ZeroRankSignals{}, ZeroRankSignals{}, time.Now())
	if specific[0].Key.Tier != 0 {
		t.Fatalf("specific query must prefer T0: %+v", specific)
	}
}

func TestZeroSignalsAreNeutral(t *testing.T) {
	g := []Group{{Key: GroupKey{Tier: 2, MemoryID: 9}, Score: 1.0}}
	Adjust(g, "hm", ZeroRankSignals{}, ZeroRankSignals{}, time.Now())
	// vague bumps 1.3; anchor/warmth must contribute NOTHING under Zero (1.0*1.3 exactly).
	if g[0].Score < 1.299 || g[0].Score > 1.301 {
		t.Fatalf("zero signals must not move the score beyond the tier prior: %v", g[0].Score)
	}
}
