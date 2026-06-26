package auth

import (
	"sync"
	"time"
)

// AttemptState is the per-identity brute-force state. "Identity" is whatever you rate-limit on,
// typically (persona, device) or the pairing code during enrollment.
type AttemptState struct {
	Failed      int       // consecutive failures since the last success
	LastAttempt time.Time // when the last attempt was evaluated
	LockedUntil time.Time // hard lockout expiry (zero = not locked)
}

// AttemptStore persists AttemptState. It must be durable across daemon restarts so an attacker
// cannot reset the counter by bouncing the process.
//
// NOTE on box-root: any store the OS can write (file, Redis on the same box) is resettable by
// root. For the realistic threat (a phone attacker with no box root) durability on disk is enough.
// To resist root you bind the counter to a TPM monotonic NV counter, or better, let the TPM's own
// dictionary-attack lockout BE the counter. See tpm.go.
type AttemptStore interface {
	Get(id string) AttemptState
	Put(id string, s AttemptState)
}

// MemoryStore is an in-process store, fine for tests and a single daemon instance. For the box,
// back this with Redis (atomic, with the state under one key) so it survives restarts and is shared
// if the daemon ever runs more than one worker.
type MemoryStore struct {
	mu sync.Mutex
	m  map[string]AttemptState
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{m: make(map[string]AttemptState)}
}

func (s *MemoryStore) Get(id string) AttemptState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id]
}

func (s *MemoryStore) Put(id string, st AttemptState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = st
}
