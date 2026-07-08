package search

// Embedding client (spec 6): plain HTTP+JSON to a llama.cpp /v1/embeddings endpoint, stdlib only.
// Vectors are normalised to unit length in Go before storage (cosine == dot thereafter, and it
// protects against a runtime returning unnormalised output). Vectors travel to Postgres in pgvector's
// text format, so the poltergres client needs nothing new.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Embedder struct {
	BaseURL string // e.g. http://127.0.0.1:18081
	ModelID string // recorded on every row (search.chunks.emb_model)
	HC      *http.Client
}

func NewEmbedder(baseURL, modelID string) *Embedder {
	return &Embedder{BaseURL: baseURL, ModelID: modelID, HC: &http.Client{Timeout: 60 * time.Second}}
}

// Embed returns one unit-normalised vector per input text.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.HC.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embeddings: http %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: got %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = normalize(d.Embedding)
	}
	return vecs, nil
}

func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	n := math.Sqrt(sum)
	if n == 0 {
		return v
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

// VecText renders a vector in pgvector's text input format: [0.1,0.2,...].
func VecText(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
