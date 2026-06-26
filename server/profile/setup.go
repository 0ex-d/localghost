package profile

import "errors"

// Slot policy: three accounts, all equal-size containers so none looks bigger.
//
//	slot 0  main account (your real data)
//	slot 1  decoy (believable data, no wipe) , the casual show
//	slot 2  decoy that wipes the main on open , the duress show
//
// All three open believable accounts; everything else is invalid (a mistype is rejected, never
// destructive). The containers are fixed equal size (see the container package), so an attacker
// imaging the disk cannot tell which holds the real data.
const (
	MainSlot   = 0
	TotalSlots = 3 // main + 2 decoys
	MaxDecoys  = 2
)

var (
	ErrNoMain    = errors.New("a main PIN is required")
	ErrNoDecoy   = errors.New("at least one decoy PIN is required so deniability has something to show")
	ErrMainSet   = errors.New("main PIN already set")
	ErrTooMany   = errors.New("at most two decoys (three slots total)")
)

// Setup builds a Registry that satisfies the policy, so an invalid layout cannot be produced.
type Setup struct {
	reg    *Registry
	main   bool
	decoys int
}

func NewSetup(boxSalt []byte) (*Setup, error) {
	r, err := NewRegistry(boxSalt)
	if err != nil {
		return nil, err
	}
	return &Setup{reg: r}, nil
}

// SetMain registers the main account PIN (slot 0). Once only.
func (s *Setup) SetMain(pin string) error {
	if s.main {
		return ErrMainSet
	}
	if err := s.reg.AddProfile(pin, MainSlot); err != nil {
		return err
	}
	s.main = true
	return nil
}

// AddDecoy registers a decoy PIN in the next slot (1, then 2). If wipesMain is true, opening it
// crypto-erases the main account (the duress decoy); otherwise it is just a believable spare.
func (s *Setup) AddDecoy(pin string, wipesMain bool) error {
	if s.decoys >= MaxDecoys {
		return ErrTooMany
	}
	slot := 1 + s.decoys // slots 1, 2
	var err error
	if wipesMain {
		err = s.reg.AddDuress(pin, slot, MainSlot)
	} else {
		err = s.reg.AddProfile(pin, slot)
	}
	if err != nil {
		return err
	}
	s.decoys++
	return nil
}

// Finalize validates the policy: a main PIN and at least one decoy. Returns the registry.
func (s *Setup) Finalize() (*Registry, error) {
	if !s.main {
		return nil, ErrNoMain
	}
	if s.decoys < 1 {
		return nil, ErrNoDecoy
	}
	return s.reg, nil
}

// DecoySlots lists the decoy data slots to pre-populate with believable content at setup.
func (s *Setup) DecoySlots() []int {
	out := make([]int, s.decoys)
	for i := 0; i < s.decoys; i++ {
		out[i] = 1 + i
	}
	return out
}
