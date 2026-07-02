package profile

import "errors"

// Slot policy: ONE real account. There is no on-disk deniability and there are no decoys , the box
// holds a single encrypted account, and deniability lives on the phone (a thin client that keeps
// only a few days of data). See the threat-model docs: we defend against phone seizure, not against
// a forensic imager of the box, so equal-size decoy containers buy nothing and are not used.
//
//	slot 0  the one real account (all your data)
//
// Two PINs exist: the MAIN pin opens slot 0, and the WIPE pin crypto-erases everything. The wipe pin
// is stored exactly like the main pin (same hashed entry shape) and padded among random filler, so
// reading the registry cannot reveal which pin wipes , or even that a second pin exists.
const (
	MainSlot = 0
)

var (
	ErrNoMain  = errors.New("a main PIN is required")
	ErrMainSet = errors.New("main PIN already set")
	ErrWipeSet = errors.New("wipe PIN already set")
)

// Setup builds a Registry for the single-account model: one main PIN, plus an optional wipe PIN.
// Building it through Setup means an invalid layout (no main PIN) cannot be produced.
type Setup struct {
	reg  *Registry
	main bool
	wipe bool
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

// SetWipe registers the wipe PIN: entering it crypto-erases the account. Optional but recommended.
// It is stored indistinguishably from the main PIN; only its action differs, and that is invisible
// until the matching PIN is entered. Choose a wipe PIN that is NOTHING like the main PIN so it cannot
// be entered by accident (this is user guidance, deliberately NOT enforced in code: an enforced
// "must differ by N" rule would itself be a tell in an open-source system).
func (s *Setup) SetWipe(pin string) error {
	if s.wipe {
		return ErrWipeSet
	}
	if err := s.reg.SetWipePin(pin); err != nil {
		return err
	}
	s.wipe = true
	return nil
}

// Finalize validates the policy: a main PIN is required; the wipe PIN is optional. Returns the
// registry.
func (s *Setup) Finalize() (*Registry, error) {
	if !s.main {
		return nil, ErrNoMain
	}
	return s.reg, nil
}
