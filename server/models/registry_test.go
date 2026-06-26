package models

import (
	"os"
	"path/filepath"
	"testing"
)

func tmpRegistry(t *testing.T, catalog string) *Registry {
	t.Helper()
	dir := t.TempDir()
	if catalog != "" {
		if err := os.WriteFile(filepath.Join(dir, "catalog.json"), []byte(catalog), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewRegistry(dir)
}

const catalog = `[
  {"id":"qwen25-3b-q4","name":"Qwen2.5 3B","detail":"small","sizeBytes":2100000000,"sha256":"aaa","file":"qwen25-3b-q4.gguf"},
  {"id":"qwen25-1_5b-q4","name":"Qwen2.5 1.5B","detail":"tiny","sizeBytes":1100000000,"sha256":"bbb","file":"qwen25-1_5b-q4.gguf"}
]`

func TestListSortedSmallestFirst(t *testing.T) {
	r := tmpRegistry(t, catalog)
	models, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "qwen25-1_5b-q4" {
		t.Fatalf("models should sort smallest-first: %+v", models)
	}
}

func TestGetUnknownIsNotFound(t *testing.T) {
	r := tmpRegistry(t, catalog)
	if _, err := r.Get("nope"); err != ErrNotFound {
		t.Fatalf("unknown model must be ErrNotFound, got %v", err)
	}
}

func TestMissingCatalogIsEmpty(t *testing.T) {
	r := tmpRegistry(t, "") // no catalog file
	models, err := r.List()
	if err != nil || len(models) != 0 {
		t.Fatalf("missing catalog should give empty list, no error: %v %+v", err, models)
	}
}

func TestPathTraversalRefused(t *testing.T) {
	r := tmpRegistry(t, catalog)
	for _, bad := range []string{"../etc/passwd", "sub/x.gguf", "/etc/passwd", ""} {
		if _, err := r.safePath(bad); err != ErrUnsafeID {
			t.Fatalf("must refuse unsafe path %q, got %v", bad, err)
		}
	}
	if _, err := r.safePath("qwen25-1_5b-q4.gguf"); err != nil {
		t.Fatalf("a plain filename must be allowed: %v", err)
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	dir := t.TempDir()
	// real file + catalog with the matching hash
	data := []byte("the model weights")
	os.WriteFile(filepath.Join(dir, "m.gguf"), data, 0o644)
	// sha256 of "the model weights"
	cat := `[{"id":"m","name":"M","detail":"d","sizeBytes":17,"sha256":"WRONGHASH","file":"m.gguf"}]`
	os.WriteFile(filepath.Join(dir, "catalog.json"), []byte(cat), 0o644)
	r := NewRegistry(dir)
	if err := r.Verify("m"); err == nil {
		t.Fatal("a wrong catalogue hash must fail verification")
	}
}
