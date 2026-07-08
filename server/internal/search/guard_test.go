package search

import (
	"strings"
	"testing"
)

// These tests are the evidence behind the SIMPLE-INLINE exception in store.go: the two queries that
// cannot be parameterized (SET LOCAL bundling, multi-statement deletion) inline ONLY values that pass
// these gates. If a gate ever loosens, these fail before the box does.

func TestValidModelIDGate(t *testing.T) {
	good := []string{"embeddinggemma-300m-q8", "model_v2.1", "A-B_c.9"}
	for _, g := range good {
		if !validModelID(g) {
			t.Fatalf("must accept %q", g)
		}
	}
	bad := []string{"", "m'; DROP TABLE search.chunks; --", "model id", "m'q", "m\"q", "m;q", "m\nq", "café"}
	for _, b := range bad {
		if validModelID(b) {
			t.Fatalf("must reject %q", b)
		}
	}
}

func TestFiltersValidateGatesSourcesAndTiers(t *testing.T) {
	if err := (Filters{Sources: []string{"email", "image"}, Tiers: []int{0, 2}}).validate(); err != nil {
		t.Fatalf("valid filters rejected: %v", err)
	}
	injections := []Filters{
		{Sources: []string{"email') OR 1=1 --"}},
		{Sources: []string{"email'"}},
		{Sources: []string{"EMAIL"}}, // case matters; the enum is exact
		{Tiers: []int{7}},
		{Tiers: []int{-1}},
	}
	for i, f := range injections {
		if err := f.validate(); err == nil {
			t.Fatalf("case %d must be rejected: %+v", i, f)
		}
	}
}

// TestVecTextCharset: the vector literal is inlined in leg B, so its output alphabet must stay inside
// digits, sign, exponent, dot, comma, brackets , nothing quotable.
func TestVecTextCharset(t *testing.T) {
	v := VecText([]float32{0.25, -1e-7, 3.5e12, 0})
	const allowed = "0123456789.,-+e[]"
	for _, c := range v {
		if !strings.ContainsRune(allowed, c) {
			t.Fatalf("VecText produced %q outside the safe alphabet in %q", c, v)
		}
	}
	if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
		t.Fatalf("not a pgvector literal: %q", v)
	}
}

// TestTierPredInlinesOnlyValidatedInts: belt-and-braces on the other inlined fragment.
func TestTierPredInlinesOnlyValidatedInts(t *testing.T) {
	if got := tierPred([]int{0, 2}); got != " AND tier IN (0,2)" {
		t.Fatalf("tierPred = %q", got)
	}
	if got := tierPred(nil); got != "" {
		t.Fatalf("empty tiers must add no predicate, got %q", got)
	}
}
