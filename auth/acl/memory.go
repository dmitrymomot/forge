package acl

import (
	"context"
	"sync"

	"github.com/dmitrymomot/forge/auth/access"
)

type (
	actionEffects   map[string]access.Effect // action -> effect
	resourceEntries map[string]actionEffects // resource id ("" = type-wide) -> actions
	typeEntries     map[string]resourceEntries
)

// memoryStore buckets entries by (tenant+subject) -> type -> id -> action, so
// EntriesFor touches only the addressed resource's bucket and the type-wide
// one — a subject with thousands of grants pays for two bucket reads, not a
// full scan.
type memoryStore struct {
	m  map[string]typeEntries // key(tenant,subject) -> entries
	mu sync.RWMutex
}

// NewMemoryStore returns an in-memory Store for tests and single-node dev use.
func NewMemoryStore() Store {
	return &memoryStore{m: map[string]typeEntries{}}
}

func memKey(tenant, subject string) string { return tenant + "\x00" + subject }

func appendEntries(dst []Entry, subject, resourceType, resourceID string, actions actionEffects) []Entry {
	for a, eff := range actions {
		dst = append(dst, Entry{Subject: subject, ResourceType: resourceType, ResourceID: resourceID, Action: a, Effect: eff})
	}
	return dst
}

func (s *memoryStore) EntriesFor(_ context.Context, tenant, subject, resourceType, resourceID string) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	resources := s.m[memKey(tenant, subject)][resourceType]
	exact, wide := resources[resourceID], resources[""]
	if resourceID == "" { // a collection check: exact IS the type-wide bucket
		exact = nil
	}
	n := len(exact) + len(wide)
	if n == 0 {
		return nil, nil
	}
	entries := make([]Entry, 0, n)
	entries = appendEntries(entries, subject, resourceType, resourceID, exact)
	entries = appendEntries(entries, subject, resourceType, "", wide)
	return entries, nil
}

func (s *memoryStore) ListFor(_ context.Context, tenant, subject string) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var entries []Entry
	for resourceType, resources := range s.m[memKey(tenant, subject)] {
		for resourceID, actions := range resources {
			entries = appendEntries(entries, subject, resourceType, resourceID, actions)
		}
	}
	return entries, nil
}

func (s *memoryStore) Put(_ context.Context, tenant string, entries []Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		k := memKey(tenant, e.Subject)
		types := s.m[k]
		if types == nil {
			types = typeEntries{}
			s.m[k] = types
		}
		resources := types[e.ResourceType]
		if resources == nil {
			resources = resourceEntries{}
			types[e.ResourceType] = resources
		}
		actions := resources[e.ResourceID]
		if actions == nil {
			actions = actionEffects{}
			resources[e.ResourceID] = actions
		}
		actions[e.Action] = e.Effect
	}
	return nil
}

func (s *memoryStore) Delete(_ context.Context, tenant, subject, resourceType, resourceID string, actionNames []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memKey(tenant, subject)
	types := s.m[k]
	resources := types[resourceType]
	actions := resources[resourceID]
	for _, a := range actionNames {
		delete(actions, a)
	}
	// reclaim emptied buckets bottom-up
	if len(actions) == 0 {
		delete(resources, resourceID)
	}
	if len(resources) == 0 {
		delete(types, resourceType)
	}
	if len(types) == 0 {
		delete(s.m, k)
	}
	return nil
}
