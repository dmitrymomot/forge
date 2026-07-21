package approval

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/dmitrymomot/forge/core/id"
)

type memoryStore struct {
	byID map[id.UUID]Request
	mu   sync.RWMutex
}

// NewMemoryStore returns an in-memory Store for tests and development. It
// is not durable: process exit loses every request.
func NewMemoryStore() Store {
	return &memoryStore{byID: make(map[id.UUID]Request)}
}

// cloneRequest copies the reference fields so callers cannot mutate stored
// state through a shared slice or map, and vice versa. Aliased Decisions
// would be a dual-control hole: a caller could rewrite a persisted vote.
func cloneRequest(r Request) Request {
	r.Decisions = slices.Clone(r.Decisions)
	r.Meta = maps.Clone(r.Meta)
	r.Payload = slices.Clone(r.Payload)
	return r
}

func (s *memoryStore) Create(_ context.Context, r Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[r.ID]; ok {
		return ErrDuplicate
	}
	s.byID[r.ID] = cloneRequest(r)
	return nil
}

func (s *memoryStore) Get(_ context.Context, reqID id.UUID) (Request, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.byID[reqID]
	if !ok {
		return Request{}, ErrNotFound
	}
	return cloneRequest(r), nil
}

func (s *memoryStore) Update(_ context.Context, r Request, expect int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.byID[r.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.Version != expect {
		return ErrConflict
	}
	next := cloneRequest(r)
	next.Version = expect + 1
	s.byID[r.ID] = next
	return nil
}

func (s *memoryStore) List(_ context.Context, f Filter) ([]Request, error) {
	s.mu.RLock()
	out := make([]Request, 0, len(s.byID))
	for _, r := range s.byID {
		if !matches(r, f) {
			continue
		}
		out = append(out, cloneRequest(r))
	}
	s.mu.RUnlock()

	// UUIDv7 ids are time-ordered, so descending id order is newest-first.
	slices.SortFunc(out, func(a, b Request) int {
		return bytes.Compare(b.ID[:], a.ID[:])
	})
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func matches(r Request, f Filter) bool {
	if f.Kind != "" && r.Kind != f.Kind {
		return false
	}
	if f.Tenant != "" && r.Tenant != f.Tenant {
		return false
	}
	if f.Requester != "" && r.Requester != f.Requester {
		return false
	}
	if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, r.Status) {
		return false
	}
	if !f.ExpiresBefore.IsZero() {
		// Rows with no expiry never match an expiry bound.
		if r.ExpiresAt.IsZero() || !r.ExpiresAt.Before(f.ExpiresBefore) {
			return false
		}
	}
	return true
}
