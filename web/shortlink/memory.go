package shortlink

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

type memoryStore struct {
	links map[string]Link
	mu    sync.RWMutex
}

// NewMemoryStore returns an in-memory Store for tests and development.
func NewMemoryStore() Store {
	return &memoryStore{links: make(map[string]Link)}
}

func (s *memoryStore) Create(_ context.Context, l Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.links[l.Code]; ok {
		return ErrDuplicate
	}
	s.links[l.Code] = l
	return nil
}

func (s *memoryStore) Get(_ context.Context, code string) (Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.links[code]
	if !ok {
		return Link{}, ErrNotFound
	}
	return l, nil
}

func (s *memoryStore) List(_ context.Context, f Filter) ([]Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Preallocate only for the unfiltered listing, where the map size is
	// exact; a tenant filter may match a small fraction of entries.
	out := []Link{}
	if f.Tenant == "" {
		out = make([]Link, 0, len(s.links))
	}
	for _, l := range s.links {
		if f.Tenant != "" && l.Tenant != f.Tenant {
			continue
		}
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b Link) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Code, b.Code)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *memoryStore) Deactivate(_ context.Context, code, tenant string, at time.Time) error {
	return s.update(code, tenant, func(l *Link) { l.DeactivatedAt = at })
}

func (s *memoryStore) Activate(_ context.Context, code, tenant string) error {
	return s.update(code, tenant, func(l *Link) { l.DeactivatedAt = time.Time{} })
}

func (s *memoryStore) Delete(_ context.Context, code, tenant string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[code]
	if !ok || (tenant != "" && l.Tenant != tenant) {
		return ErrNotFound
	}
	delete(s.links, code)
	return nil
}

func (s *memoryStore) update(code, tenant string, fn func(*Link)) error {
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
