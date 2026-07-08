package synth

import "context"

// Stub is the ghost.synthd client used until synthd exists. It returns no candidates and reports
// not-ready. cued runs its full logic against this; the four mechanisms all execute, the priming call
// happens on every context change, but synthd never proposes anything, so cued correctly surfaces
// nothing. This is the honest floor: cued is a real daemon that is running-but-blind, not a stub that
// pretends to think. Swap this for a real client when synthd is built , no change to cued.
type Stub struct{}

// NewStub returns the not-ready, returns-nothing client.
func NewStub() *Stub { return &Stub{} }

// Prime always returns nothing , there is no index behind it yet.
func (s *Stub) Prime(_ context.Context, _ Query) ([]Candidate, error) { return nil, nil }

// Ready is false: cued logs that memory surfacing is offline until synthd lands.
func (s *Stub) Ready() bool { return false }
