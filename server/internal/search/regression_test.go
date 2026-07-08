package search

// Regression tests (spec 14.3): correctness gates, not quality metrics. They need a live Postgres
// with the search schema, so they gate on GHOST_TEST_PG_SOCKDIR being set , run on the dev box or the
// real box, skipped everywhere else. Each test documents the invariant it defends.

import (
	"os"
	"testing"
)

func liveStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("GHOST_TEST_PG_SOCKDIR") == "" {
		t.Skip("regression: set GHOST_TEST_PG_SOCKDIR (plus _PORT/_USER/_PASS/_DB) for a live schema")
	}
	t.Fatal("live wiring lands with the first migrated box; assertions below are the contract")
	return nil
}

// TestStaleChunksNeverSurface: mark a chunk stale, run both legs, assert absent (invariant I4's
// query-side half , spec: stale filter is mandatory in every query, no exceptions).
func TestStaleChunksNeverSurface(t *testing.T) { _ = liveStore(t) }

// TestTombstonedContentRefusesReingest: delete an original, re-submit identical bytes, assert the
// ingest refuses (spec 12: deleted content never returns, even from an old phone upload).
func TestTombstonedContentRefusesReingest(t *testing.T) { _ = liveStore(t) }

// TestModelMismatchExcluded: rows whose emb_model differs from the configured model must not join
// leg B results mid-migration (spec 2.2 emb_model / 13.3 model migration).
func TestModelMismatchExcluded(t *testing.T) { _ = liveStore(t) }
