package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Store is the fleet-dedup seam: every instance computes the same tick times,
// and Claim is the insert race that lets exactly one of them enqueue each
// tick — one Claim per (name, scheduledFor instant) succeeds, the rest get
// ErrAlreadyClaimed. Release undoes a claim after a failed enqueue so the
// tick can be retried; PurgeBefore deletes claims scheduled strictly before
// cutoff and returns how many were removed. Implementations key claims by
// the absolute instant, not the wall-clock representation.
type Store interface {
	Claim(ctx context.Context, name string, scheduledFor time.Time) error
	Release(ctx context.Context, name string, scheduledFor time.Time) error
	PurgeBefore(ctx context.Context, cutoff time.Time) (int, error)
}

// MemoryStore is the in-process Store: correct for a single instance and for
// tests, invisible to other instances — a fleet shares a durable store
// (async/scheduler/postgres) instead.
type MemoryStore struct {
	claims map[memoryClaim]struct{}
	mu     sync.Mutex
}

type memoryClaim struct {
	name string
	tick int64
}

// NewMemoryStore builds an empty in-memory claim store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{claims: make(map[memoryClaim]struct{})}
}

// Claim implements Store.
func (s *MemoryStore) Claim(_ context.Context, name string, scheduledFor time.Time) error {
	if name == "" {
		return errors.New("scheduler: Claim requires a non-empty job name")
	}
	key := memoryClaim{name: name, tick: scheduledFor.UnixNano()}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.claims[key]; ok {
		return ErrAlreadyClaimed
	}
	s.claims[key] = struct{}{}
	return nil
}

// Release implements Store.
func (s *MemoryStore) Release(_ context.Context, name string, scheduledFor time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.claims, memoryClaim{name: name, tick: scheduledFor.UnixNano()})
	return nil
}

// PurgeBefore implements Store.
func (s *MemoryStore) PurgeBefore(_ context.Context, cutoff time.Time) (int, error) {
	limit := cutoff.UnixNano()
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for key := range s.claims {
		if key.tick < limit {
			delete(s.claims, key)
			n++
		}
	}
	return n, nil
}
