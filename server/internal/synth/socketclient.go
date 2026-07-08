package synth

// SocketClient is the real synth.Client: it talks to ghost.synthd over its control socket, calling the
// "prime" command. cued uses this instead of the Stub once synthd is running. It is still cheap and
// frequent , a unix-socket round trip per context change , and returns whatever synthd's query
// pipeline yields (nothing, until synthd's index is built).
//
// This keeps ONE protocol for everything: cued->synthd is the same ctlsock every operator command and
// inter-daemon call uses, not a second bespoke channel.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
)

type SocketClient struct {
	c *ctlsock.Client
}

// NewSocketClient targets ghost.synthd's socket under runDir (<mount>/run). The timeout is short:
// priming is a hot path and cued would rather get nothing quickly than block on a slow retrieval.
func NewSocketClient(runDir string, timeout time.Duration) *SocketClient {
	return NewSocketClientFor("ghost.synthd", runDir, timeout)
}

// NewSocketClientFor targets any service speaking the prime/ready contract , ghost.searchd answers it
// wire-compatibly, so cued's provider is a conf key, not a code change. Which daemon owns ambient
// retrieval long-term is the operator's decision; this just removes the friction from making it.
func NewSocketClientFor(service, runDir string, timeout time.Duration) *SocketClient {
	return &SocketClient{c: ctlsock.NewClientTimeout(service, runDir, timeout)}
}

// Prime calls synthd's "prime" command with the context query and decodes the candidates.
func (s *SocketClient) Prime(_ context.Context, q Query) ([]Candidate, error) {
	resp, err := s.c.Call("prime", q)
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, nil
	}
	var cands []Candidate
	if err := json.Unmarshal(resp.Data, &cands); err != nil {
		return nil, err
	}
	return cands, nil
}

// Ready calls synthd's "ready" command; a transport error or a false reply both read as not-ready, so
// cued treats an unreachable synthd the same as an unbuilt one , stay silent.
func (s *SocketClient) Ready() bool {
	resp, err := s.c.Call("ready", nil)
	if err != nil || len(resp.Data) == 0 {
		return false
	}
	var r struct {
		Ready bool `json:"ready"`
	}
	if err := json.Unmarshal(resp.Data, &r); err != nil {
		return false
	}
	return r.Ready
}
