package synthd

import (
	"context"

	"github.com/LocalGhostDao/localghost/server/internal/synth"
)

// EmptyIndex is the Index used until the real corpus exists. It holds nothing and reports not-ready,
// so synthd's query pipeline runs in full and returns nothing , running-but-blind, not faking a
// corpus. Swap it for the real embedded index behind the Index interface when the indexing work lands.
type EmptyIndex struct{}

// NewEmptyIndex returns the not-ready, returns-nothing index.
func NewEmptyIndex() *EmptyIndex { return &EmptyIndex{} }

func (EmptyIndex) Lookup(context.Context, synth.Query) ([]Match, error) { return nil, nil }
func (EmptyIndex) Ready() bool                                          { return false }
func (EmptyIndex) Size() int                                            { return 0 }
