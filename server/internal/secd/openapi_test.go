package secd

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// decodeStrict unmarshals live handler output into a doc type with DisallowUnknownFields: if the
// handler emits a field the doc type does not declare, the test fails , that is the anti-drift
// contract between the ad-hoc map responses and the documented shapes.
func decodeStrict(t *testing.T, body []byte, into any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		t.Fatalf("live response does not match documented shape: %v\nbody: %s", err, body)
	}
}

func TestOpenAPICoversEveryRoute(t *testing.T) {
	s := newTestServer(t)
	doc := s.openAPIDocument()
	// It must marshal to valid JSON at all.
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("spec does not marshal: %v", err)
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec has no paths")
	}
	// Every route in the table appears in the spec (structural, since both come from routes(),
	// but this catches a path-collision overwriting another in the paths map).
	for _, rt := range s.routes() {
		if _, ok := paths[rt.Path]; !ok {
			t.Fatalf("route %s missing from spec", rt.Path)
		}
	}
	// Both auth layers are documented: edge mTLS and the PIN-derived session bearer.
	for _, must := range []string{"mutualTLS", "bearer", "appears-down"} {
		if !strings.Contains(string(raw), must) {
			t.Fatalf("spec missing %q", must)
		}
	}
}

func TestOpenAPIServedOnItsOwnRoute(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/v1/openapi.json", nil))
	if rr.Code != 200 {
		t.Fatalf("openapi route: %d", rr.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("served spec is not JSON: %v", err)
	}
	if doc["openapi"] != "3.0.3" {
		t.Fatalf("unexpected openapi version: %v", doc["openapi"])
	}
}

// TestDocumentedShapesMatchLiveHandlers pins the doc types against what the handlers actually emit,
// for every endpoint reachable in a unit test on the sim backend. Endpoints needing a mounted
// account (notifications, mute) are exercised the same way once their stores are testable; until
// then their spec entries are deliberately loose rather than guessed.
func TestDocumentedShapesMatchLiveHandlers(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()

	get := func(path string) []byte {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		return rr.Body.Bytes()
	}

	decodeStrict(t, get("/v1/health"), &healthDoc{})
	decodeStrict(t, get("/v1/info"), &infoDoc{})          // locked shape
	decodeStrict(t, get("/v1/models"), &modelsDoc{})      // empty catalogue
	decodeStrict(t, get("/v1/unlock/poll"), &unlockPollDoc{}) // idle: empty stages, not done

	// Unlock start on the sim backend.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/v1/unlock", strings.NewReader(`{"pin":"0000"}`)))
	if rr.Code == 200 {
		decodeStrict(t, rr.Body.Bytes(), &unlockStartDoc{})
	}
}
