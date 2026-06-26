package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Phone-runnable models live in the UNENCRYPTED system area, not in any account's container. They
// are public artifacts, identical for every account, the same as the daemon binaries, so encrypting
// them per slot would buy nothing and cost three copies of multi-gigabyte weights. They sit beside
// where the apps run, shared across all accounts, and ghost.secd serves the catalogue and the bytes
// to the phone, which downloads a model and runs it locally.
//
// Because the model area is unencrypted and shared, reading or serving a model needs NO mounted
// account and NO PIN , it is available the same way regardless of which account (if any) is open.
// It carries no personal data, so this does not weaken the per-account isolation.

// Model is one phone-runnable model. The JSON shape matches what the app expects (id, name, detail,
// sizeBytes, sha256), so ghost.secd's catalogue maps straight onto the app's PhoneModel.
type Model struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Detail    string `json:"detail"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	File      string `json:"file"` // filename within the models dir (not sent to the phone)
}

// Registry reads the catalogue from the unencrypted models directory. dir is the shared system path,
// e.g. /var/lib/ghost/models, NOT inside any account container.
type Registry struct {
	dir string
}

func NewRegistry(dir string) *Registry { return &Registry{dir: dir} }

var (
	ErrNotFound = errors.New("model not found")
	ErrUnsafeID = errors.New("invalid model id")
)

// catalogPath is the manifest listing the available models.
func (r *Registry) catalogPath() string { return filepath.Join(r.dir, "catalog.json") }

// List returns the available models, sorted by size (smallest first, friendliest for a phone). It
// reads the manifest from the unencrypted area; no account need be mounted.
func (r *Registry) List() ([]Model, error) {
	b, err := os.ReadFile(r.catalogPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []Model{}, nil // no models installed yet
		}
		return nil, err
	}
	var models []Model
	if err := json.Unmarshal(b, &models); err != nil {
		return nil, err
	}
	sort.Slice(models, func(i, j int) bool { return models[i].SizeBytes < models[j].SizeBytes })
	return models, nil
}

// Get returns one model's metadata by id.
func (r *Registry) Get(id string) (Model, error) {
	models, err := r.List()
	if err != nil {
		return Model{}, err
	}
	for _, m := range models {
		if m.ID == id {
			return m, nil
		}
	}
	return Model{}, ErrNotFound
}

// Open returns a reader for a model's bytes, for streaming to the phone (the daemon adds HTTP Range
// support for resumable downloads on top of this). It validates the id maps to a real catalogue
// entry and that the resolved path stays inside the models dir, so a crafted id cannot read
// arbitrary files.
func (r *Registry) Open(id string) (io.ReadCloser, int64, error) {
	m, err := r.Get(id)
	if err != nil {
		return nil, 0, err
	}
	path, err := r.safePath(m.File)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// safePath resolves a catalogue filename within the models dir and refuses anything that escapes it
// (path traversal guard). Only a plain filename inside dir is allowed.
func (r *Registry) safePath(file string) (string, error) {
	if file == "" || filepath.Base(file) != file {
		return "", ErrUnsafeID
	}
	full := filepath.Join(r.dir, file)
	rel, err := filepath.Rel(r.dir, full)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", ErrUnsafeID
	}
	return full, nil
}

// Verify recomputes a model file's SHA-256 and checks it against the catalogue, so an operator can
// confirm an installed model is intact and unmodified. The phone also verifies after download.
func (r *Registry) Verify(id string) error {
	m, err := r.Get(id)
	if err != nil {
		return err
	}
	if m.SHA256 == "" {
		return fmt.Errorf("no expected hash for %s", id)
	}
	rc, _, err := r.Open(id)
	if err != nil {
		return err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != m.SHA256 {
		return fmt.Errorf("hash mismatch for %s: catalogue %s, file %s", id, m.SHA256, got)
	}
	return nil
}
