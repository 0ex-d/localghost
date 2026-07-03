package secd

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
)

// OpenAPI generation, from scratch (no swaggo, no oapi-codegen , the spec for a dozen routes does
// not justify a dependency). The design goal is that the spec CANNOT silently drift from the code:
//
//  1. The route table below is the single source of truth. Handler() builds the mux FROM it, so a
//     route cannot exist in the server without existing in the spec, or vice versa , that guarantee
//     is structural, not a convention.
//  2. Request/response shapes are documented as Go types (below) and turned into schemas by
//     reflection over their json tags. Where a handler still builds ad-hoc maps, the doc type is
//     the contract and openapi_test.go calls the LIVE handler and unmarshals its output into the
//     doc type with DisallowUnknownFields , if the handler grows or renames a field, the build
//     fails until the doc type follows. Shapes that cannot be exercised in a unit test are marked
//     loose (additionalProperties) rather than guessed at; a wrong spec is worse than a vague one.
//
// The spec is served at /v1/openapi.json , behind the same edge as everything else (device cert or
// uniform 503), so it documents the API to enrolled devices without describing the box to anyone
// else.

// route is one entry of the single source of truth.
type route struct {
	Method   string // primary method for the spec; handlers still enforce their own
	Path     string
	Summary  string
	Auth     bool // requires the bearer session token from a PIN unlock
	Request  any  // zero value of the request doc type; nil = no body
	Response any  // zero value of the response doc type; nil = binary/stream
	Binary   bool // response is raw bytes (model download)
	Handler  http.HandlerFunc
}

// --- documented wire shapes -------------------------------------------------------------------
// These document what the handlers actually emit. The ones exercised by openapi_test.go are hard
// contracts (DisallowUnknownFields against live output); the rest follow the same rule as soon as
// their backing stores are testable.

type healthDoc struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
}

type infoDoc struct {
	Locked      bool `json:"locked"`
	MountedSlot *int `json:"mountedSlot,omitempty"`
	Daemons     *int `json:"daemons,omitempty"`
}

type stageDoc struct {
	Stage string `json:"stage"`
	State string `json:"state"`
}

type lockDoc struct {
	Locked bool       `json:"locked"`
	Steps  []stageDoc `json:"steps"`
	Error  string     `json:"error,omitempty"`
}

type unlockStartDoc struct {
	Started bool `json:"started"`
}

type unlockPollDoc struct {
	Stages []stageDoc `json:"stages"`
	Done   bool       `json:"done"`
	Failed string     `json:"failed,omitempty"`
	Token  string     `json:"token,omitempty"` // issued once, on a successful unlock
}

type okDoc struct {
	OK bool `json:"ok"`
}

type muteStatusDoc struct {
	Mutes map[string]int64 `json:"mutes"` // scope -> muted-until unix seconds
}

type muteRequestDoc struct {
	Scope   string `json:"scope"`
	Preset  string `json:"preset,omitempty"`
	Minutes int    `json:"minutes,omitempty"`
	Hours   int    `json:"hours,omitempty"`
	Days    int    `json:"days,omitempty"`
	Clear   bool   `json:"clear,omitempty"`
}

type idRequestDoc struct {
	ID int64 `json:"id"`
}

type answerRequestDoc struct {
	ID     int64  `json:"id"`
	Answer string `json:"answer"`
}

// looseList documents endpoints whose element shape is owned by the in-volume notification store
// and is not yet exercisable in a unit test. Stated plainly rather than guessed: the schema says
// "array of objects" until the store settles and the shape can be pinned like the others.
type looseList struct {
	Available     *bool            `json:"available,omitempty"`
	Notifications []map[string]any `json:"notifications"`
}

type modelDoc struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Detail    string `json:"detail"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

type modelsDoc struct {
	Models []modelDoc `json:"models"`
}

// routes is the single source of truth: mux registration and the OpenAPI document both derive from
// this table and nothing else.
func (s *Server) routes() []route {
	return []route{
		{Method: "GET", Path: "/v1/health", Summary: "Cheap reachability check; no account, no session.",
			Response: healthDoc{}, Handler: s.handleHealth},
		{Method: "POST", Path: "/v1/unlock", Summary: "Start a PIN unlock; progress is read at /v1/unlock/poll.",
			Request: unlockRequest{}, Response: unlockStartDoc{}, Handler: s.handleUnlockStart},
		{Method: "GET", Path: "/v1/unlock/poll", Summary: "Unlock progress; on success carries the fresh session token.",
			Response: unlockPollDoc{}, Handler: s.handleUnlockPoll},
		{Method: "POST", Path: "/v1/lock", Summary: "Spin the box down: stop DBs, unmount, close LUKS, revoke the session.",
			Auth: true, Response: lockDoc{}, Handler: s.handleLock},
		{Method: "GET", Path: "/v1/info", Summary: "Box + mounted-account summary for the home screen.",
			Response: infoDoc{}, Handler: s.handleInfo},
		{Method: "GET", Path: "/v1/notifications", Summary: "Poll pending notifications for the mounted account.",
			Auth: true, Response: looseList{}, Handler: s.handleNotifications},
		{Method: "GET", Path: "/v1/notifications/mute", Summary: "Current notification mutes per scope.",
			Auth: true, Response: muteStatusDoc{}, Handler: s.handleMute},
		{Method: "POST", Path: "/v1/notifications/mute", Summary: "Set or clear a notification mute for a scope.",
			Auth: true, Request: muteRequestDoc{}, Response: okDoc{}, Handler: s.handleMute},
		{Method: "GET", Path: "/v1/notifications/list", Summary: "Full notification history for the mounted account.",
			Auth: true, Response: looseList{}, Handler: s.handleNotificationList},
		{Method: "POST", Path: "/v1/notifications/seen", Summary: "Mark a notification seen.",
			Auth: true, Request: idRequestDoc{}, Response: okDoc{}, Handler: s.handleNotificationSeen},
		{Method: "POST", Path: "/v1/notifications/delete", Summary: "Delete a notification.",
			Auth: true, Request: idRequestDoc{}, Response: okDoc{}, Handler: s.handleNotificationDelete},
		{Method: "POST", Path: "/v1/notifications/answer", Summary: "Answer a notification's question.",
			Auth: true, Request: answerRequestDoc{}, Response: okDoc{}, Handler: s.handleNotificationAnswer},
		{Method: "GET", Path: "/v1/models", Summary: "The model catalogue the box offers the phone.",
			Response: modelsDoc{}, Handler: s.handleModels},
		{Method: "GET", Path: "/v1/models/", Summary: "Model bytes by id (/v1/models/{id}); a large binary download.",
			Binary: true, Handler: s.handleModelBytes},
		{Method: "GET", Path: "/v1/openapi.json", Summary: "This document.",
			Handler: s.handleOpenAPI},
	}
}

// handleOpenAPI serves the generated spec. Behind the cert-gated edge like every other route, so it
// documents the API to enrolled devices and describes nothing to anyone else.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.openAPIDocument())
}

// openAPIDocument builds an OpenAPI 3.0 document from the route table by reflection.
func (s *Server) openAPIDocument() map[string]any {
	paths := map[string]any{}
	for _, rt := range s.routes() {
		op := map[string]any{
			"summary":   rt.Summary,
			"responses": map[string]any{},
		}
		responses := op["responses"].(map[string]any)
		switch {
		case rt.Binary:
			responses["200"] = map[string]any{
				"description": "raw bytes",
				"content": map[string]any{
					"application/octet-stream": map[string]any{
						"schema": map[string]any{"type": "string", "format": "binary"},
					},
				},
			}
		case rt.Response != nil:
			responses["200"] = map[string]any{
				"description": "OK",
				"content": map[string]any{
					"application/json": map[string]any{"schema": schemaOf(reflect.TypeOf(rt.Response))},
				},
			}
		default:
			responses["200"] = map[string]any{"description": "OK"}
		}
		// The edge's appears-down behaviour, documented honestly: anything unauthenticated ,
		// missing cert at nginx, missing session here , is a generic unavailable, never a 401/403.
		responses["503"] = map[string]any{
			"description": "appears-down: unauthenticated, locked, or genuinely unavailable , deliberately indistinguishable",
		}
		if rt.Request != nil {
			op["requestBody"] = map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{"schema": schemaOf(reflect.TypeOf(rt.Request))},
				},
			}
		}
		if rt.Auth {
			op["security"] = []any{map[string]any{"session": []any{}}}
		}
		item, ok := paths[rt.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[rt.Path] = item
		}
		item[strings.ToLower(rt.Method)] = op
	}
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "ghost.secd",
			"version":     "1",
			"description": "The LocalGhost box API. Transport auth is a box-issued device certificate at the nginx edge (mTLS); requests without it never reach this daemon. Session auth on top is a bearer token issued by a successful PIN unlock.",
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"session": map[string]any{"type": "http", "scheme": "bearer",
					"description": "session token from /v1/unlock/poll after a correct PIN"},
				"deviceCert": map[string]any{"type": "mutualTLS",
					"description": "box-issued device certificate, delivered in the enrolment QR; enforced by nginx before this daemon"},
			},
		},
		"security": []any{map[string]any{"deviceCert": []any{}}},
		"paths":    paths,
	}
}

// schemaOf turns a Go type into a JSON schema by reflection over json tags. Deliberately covers
// only what the doc types use: structs, pointers, slices, maps, strings, bools, ints, floats.
func schemaOf(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.Pointer:
		return schemaOf(t.Elem()) // optionality is expressed via required-list absence
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaOf(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaOf(t.Elem())}
	case reflect.Interface:
		return map[string]any{} // any
	case reflect.Struct:
		props := map[string]any{}
		var required []string
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name := f.Name
			omitempty := false
			if tag != "" {
				parts := strings.Split(tag, ",")
				if parts[0] != "" {
					name = parts[0]
				}
				for _, p := range parts[1:] {
					if p == "omitempty" {
						omitempty = true
					}
				}
			}
			props[name] = schemaOf(f.Type)
			if !omitempty && f.Type.Kind() != reflect.Pointer {
				required = append(required, name)
			}
		}
		out := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			sort.Strings(required)
			out["required"] = required
		}
		return out
	default:
		return map[string]any{}
	}
}
