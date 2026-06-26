package integration

// Set is the integrations belonging to ONE mounted account. It is constructed only from that
// account's decrypted store, so by construction it can never see another account's connectors. The
// daemon holds a Set only for the currently-mounted account; unmounting drops it and zeroises the
// secrets.
//
// isDecoy controls the safe default: a new integration on a decoy starts Paused (configured, not
// polling), so a decoy reads as set-up-but-unused rather than showing live token refreshes a stale
// account would not. On the main, Add still defaults to Paused and you Enable explicitly, so nothing
// goes live by accident.
type Set struct {
	slot    int
	isDecoy bool
	items   map[string]*Integration
}

func NewSet(slot int, isDecoy bool) *Set {
	return &Set{slot: slot, isDecoy: isDecoy, items: make(map[string]*Integration)}
}

func (s *Set) Slot() int     { return s.slot }
func (s *Set) IsDecoy() bool { return s.isDecoy }

// Add registers a connector. It always starts Paused; enabling is a separate, explicit step, so no
// integration begins live polling the moment it is added. On a decoy it MUST stay Paused, see
// Enable.
func (s *Set) Add(id string, kind Kind, label string, secret []byte) (*Integration, error) {
	if _, ok := s.items[id]; ok {
		return nil, ErrExists
	}
	it := &Integration{ID: id, Kind: kind, Label: label, State: Paused, Secret: secret}
	s.items[id] = it
	return it, nil
}

// Enable turns an integration live (token refresh, polling). Independent per integration. On a decoy
// this is refused: a decoy with a live, refreshing connector is the behavioural tell we are avoiding,
// so decoy integrations stay configured-but-paused. Enable a connector on the main only.
func (s *Set) Enable(id string) error {
	it, ok := s.items[id]
	if !ok {
		return ErrNotFound
	}
	if s.isDecoy {
		return ErrDecoyStaysPaused
	}
	it.State = Enabled
	return nil
}

// Disable pauses an integration without removing it. Allowed on any account; pausing is always safe.
func (s *Set) Disable(id string) error {
	it, ok := s.items[id]
	if !ok {
		return ErrNotFound
	}
	it.State = Paused
	return nil
}

// Remove deletes a connector and zeroises its secret.
func (s *Set) Remove(id string) error {
	it, ok := s.items[id]
	if !ok {
		return ErrNotFound
	}
	zero(it.Secret)
	delete(s.items, id)
	return nil
}

// Active returns the integrations that should be doing live background work right now. For a decoy
// this is always empty (nothing is Enabled), so the daemons start no background polling for a decoy.
func (s *Set) Active() []*Integration {
	out := make([]*Integration, 0, len(s.items))
	for _, it := range s.items {
		if it.Polls() {
			out = append(out, it)
		}
	}
	return out
}

// All returns every integration (for display in settings), Paused and Enabled alike.
func (s *Set) All() []*Integration {
	out := make([]*Integration, 0, len(s.items))
	for _, it := range s.items {
		out = append(out, it)
	}
	return out
}

// Zeroise clears all secrets, called on unmount so no token lingers in memory.
func (s *Set) Zeroise() {
	for _, it := range s.items {
		zero(it.Secret)
	}
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
