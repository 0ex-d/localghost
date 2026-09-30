package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An input the embeddings server refuses no longer fails its whole batch for good: the batch is
// tried input by input, and the long one is cut until it fits.
func TestEmbedFittingCutsWhatTheServerRefuses(t *testing.T) {
	const limit = 100 // runes the fake server takes in one input
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, in := range req.Input {
			if len([]rune(in)) > limit {
				http.Error(w, `{"error":{"message":"input (412 tokens) is too large to process. increase the physical batch size"}}`, 500)
				return
			}
		}
		out := map[string]any{}
		var data []map[string]any
		for range req.Input {
			data = append(data, map[string]any{"embedding": []float32{3, 4}})
		}
		out["data"] = data
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	e := NewEmbedder(srv.URL, "test")

	vecs, cut, err := e.EmbedFitting(context.Background(), []string{"short", strings.Repeat("ab ", 150), "also short"})
	if err != nil || len(vecs) != 3 || cut != 1 {
		t.Fatalf("vecs %d cut %d err %v", len(vecs), cut, err)
	}
	if vecs[1][0] != 0.6 || vecs[1][1] != 0.8 {
		t.Fatalf("the cut chunk's vector %v", vecs[1])
	}
	// a batch the server takes whole is one call
	calls = 0
	if _, cut, err := e.EmbedFitting(context.Background(), []string{"a", "b"}); err != nil || cut != 0 || calls != 1 {
		t.Fatalf("whole batch: cut %d calls %d err %v", cut, calls, err)
	}
	// the refusal carries the server's words
	if _, err := e.Embed(context.Background(), []string{strings.Repeat("x", 200)}); err == nil || !strings.Contains(err.Error(), "too large to process") {
		t.Fatalf("error %v", err)
	}
	// an unreachable server is not tried input by input
	down := NewEmbedder("http://127.0.0.1:1", "test")
	if _, _, err := down.EmbedFitting(context.Background(), []string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("down: %v", err)
	}
}
