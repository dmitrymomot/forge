package smartlink

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"
	"time"
)

// MemoryStore is an in-memory [Store] for tests and development. Its
// behavior is the reference contract other Store implementations must match.
type MemoryStore struct {
	links map[string]Link
	mu    sync.RWMutex
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{links: make(map[string]Link)}
}

// cloneLink copies l's Metadata so callers cannot mutate stored state
// through a shared map (and vice versa).
func cloneLink(l Link) Link {
	l.Metadata = maps.Clone(l.Metadata)
	return l
}

// Create implements Store.
func (s *MemoryStore) Create(_ context.Context, l Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.links[l.Code]; ok {
		return ErrDuplicate
	}
	s.links[l.Code] = cloneLink(l)
	return nil
}

// Get implements Store.
func (s *MemoryStore) Get(_ context.Context, code string) (Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.links[code]
	if !ok {
		return Link{}, ErrNotFound
	}
	return cloneLink(l), nil
}

// List implements Store.
func (s *MemoryStore) List(_ context.Context, f Filter) ([]Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Link, 0, len(s.links))
	for _, l := range s.links {
		if f.Tenant != "" && l.Tenant != f.Tenant {
			continue
		}
		out = append(out, cloneLink(l))
	}
	slices.SortFunc(out, func(a, b Link) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Code, b.Code)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// Deactivate implements Store. A zero at is a no-op on DeactivatedAt (it
// never reactivates an already-deactivated link), but the existence and
// tenant predicate are still enforced.
func (s *MemoryStore) Deactivate(_ context.Context, code, tenant string, at time.Time) error {
	return s.mutate(code, tenant, func(l *Link) {
		if !at.IsZero() {
			l.DeactivatedAt = at
		}
	})
}

// Activate implements Store.
func (s *MemoryStore) Activate(_ context.Context, code, tenant string) error {
	return s.mutate(code, tenant, func(l *Link) { l.DeactivatedAt = time.Time{} })
}

// Delete implements Store.
func (s *MemoryStore) Delete(_ context.Context, code, tenant string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[code]
	if !ok || (tenant != "" && l.Tenant != tenant) {
		return ErrNotFound
	}
	delete(s.links, code)
	return nil
}

// mutate applies fn to the record at code under the write lock, after
// checking existence and the tenant predicate atomically with the mutation.
func (s *MemoryStore) mutate(code, tenant string, fn func(*Link)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[code]
	if !ok || (tenant != "" && l.Tenant != tenant) {
		return ErrNotFound
	}
	fn(&l)
	s.links[code] = l
	return nil
}
