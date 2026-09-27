package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPickEmbedModelPrefersTheMirrorsBuildAndKeepsTheOldOne(t *testing.T) {
	dir := t.TempDir()
	// nothing there: the mirror's name, so "no weights" names the file to provide
	if p, id := pickEmbedModel(dir); filepath.Base(p) != "embeddinggemma-300m-qat-Q8_0.gguf" || id != "embeddinggemma-300m-qat-Q8_0" {
		t.Fatalf("empty: %s %s", p, id)
	}
	// an older box: only the q8 it was set up with
	os.WriteFile(filepath.Join(dir, "embeddinggemma-300m-q8.gguf"), []byte("gguf"), 0o644)
	if p, id := pickEmbedModel(dir); filepath.Base(p) != "embeddinggemma-300m-q8.gguf" || id != "embeddinggemma-300m-q8" {
		t.Fatalf("old only: %s %s", p, id)
	}
	// an empty file is not a model
	os.WriteFile(filepath.Join(dir, "embeddinggemma-300m-qat-Q8_0.gguf"), nil, 0o644)
	if _, id := pickEmbedModel(dir); id != "embeddinggemma-300m-q8" {
		t.Fatalf("empty qat file picked: %s", id)
	}
	// both: the mirror's build wins (and searchd re-embeds what the old one wrote)
	os.WriteFile(filepath.Join(dir, "embeddinggemma-300m-qat-Q8_0.gguf"), []byte("gguf"), 0o644)
	if _, id := pickEmbedModel(dir); id != "embeddinggemma-300m-qat-Q8_0" {
		t.Fatalf("both: %s", id)
	}
	c := defaultConf(filepath.Dir(dir))
	if c.EmbedModelID == "" || c.EmbedModelPath == "" {
		t.Fatalf("defaultConf: %+v", c)
	}
}
