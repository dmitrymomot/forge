package rbac

import (
	"context"
	"maps"
	"slices"
	"sync"
)

type memoryStore struct {
	m  map[string]map[string]struct{} // key(tenant,subject) -> role set
	mu sync.RWMutex
}

// NewMemoryStore returns an in-memory Store for tests and single-node dev use.
func NewMemoryStore() Store {
	return &memoryStore{m: map[string]map[string]struct{}{}}
}

func memKey(tenant, subject string) string { return tenant + "\x00" + subject }

func (s *memoryStore) RolesFor(_ context.Context, tenant, subject string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := s.m[memKey(tenant, subject)]
	if len(set) == 0 {
		return nil, nil
	}
	return slices.Collect(maps.Keys(set)), nil
}

func (s *memoryStore) Assign(_ context.Context, tenant, subject string, roles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memKey(tenant, subject)
	set := s.m[k]
	if set == nil {
		set = map[string]struct{}{}
		s.m[k] = set
	}
	for _, r := range roles {
		set[r] = struct{}{}
	}
	return nil
}

func (s *memoryStore) Unassign(_ context.Context, tenant, subject string, roles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := s.m[memKey(tenant, subject)]
	for _, r := range roles {
		delete(set, r)
	}
	return nil
}
