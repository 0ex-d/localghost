package integration

import "encoding/json"

// Mounting an account decrypts its store; the integrations live inside that plaintext. Load builds
// the Set from the just-mounted account's bytes. There is no global integration store and no other
// constructor that crosses accounts, so a Set always belongs to exactly the account that was
// unlocked.
//
// blob is the integrations section of the account's decrypted store. On unmount, call Set.Zeroise
// then drop the Set, so no token survives in memory past the account being open.
func Load(slot int, blob []byte) (*Set, error) {
	s := NewSet(slot)
	if len(blob) == 0 {
		return s, nil
	}
	var stored []storedIntegration
	if err := json.Unmarshal(blob, &stored); err != nil {
		return nil, err
	}
	for _, si := range stored {
		it := &Integration{ID: si.ID, Kind: si.Kind, Label: si.Label, State: si.State, Secret: si.Secret}
		s.items[si.ID] = it
	}
	return s, nil
}

// Save serialises the Set back into the account's store blob (to be re-encrypted with the account's
// key by the caller). Only ever writes the mounted account's own integrations.
func (s *Set) Save() ([]byte, error) {
	out := make([]storedIntegration, 0, len(s.items))
	for _, it := range s.items {
		out = append(out, storedIntegration{
			ID: it.ID, Kind: it.Kind, Label: it.Label, State: it.State, Secret: it.Secret,
		})
	}
	return json.Marshal(out)
}

type storedIntegration struct {
	ID     string `json:"id"`
	Kind   Kind   `json:"kind"`
	Label  string `json:"label"`
	State  State  `json:"state"`
	Secret []byte `json:"secret"`
}
