package apikey

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

type memoryStore struct {
	byID   map[id.UUID]Key
	byHash map[string]id.UUID
	mu     sync.RWMutex
}

// NewMemoryStore returns an in-memory Store for tests and development.
func NewMemoryStore() Store {
	return &memoryStore{
		byID:   make(map[id.UUID]Key),
		byHash: make(map[string]id.UUID),
	}
}

// cloneKey copies the record's reference fields so callers cannot mutate
// stored state through shared slices/maps (and vice versa).
func cloneKey(k Key) Key {
	k.Scopes = slices.Clone(k.Scopes)
	k.Meta = maps.Clone(k.Meta)
	return k
}

func (s *memoryStore) Create(_ context.Context, k Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[k.ID]; ok {
		return ErrDuplicate
	}
	if _, ok := s.byHash[k.Hash]; ok {
		return ErrDuplicate
	}
	s.byID[k.ID] = cloneKey(k)
	s.byHash[k.Hash] = k.ID
	return nil
}

func (s *memoryStore) Get(_ context.Context, keyID id.UUID) (Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.byID[keyID]
	if !ok {
		return Key{}, ErrNotFound
	}
	return cloneKey(k), nil
}

func (s *memoryStore) GetByHash(_ context.Context, hash string) (Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyID, ok := s.byHash[hash]
	if !ok {
		return Key{}, ErrNotFound
	}
	return cloneKey(s.byID[keyID]), nil
}

func (s *memoryStore) List(_ context.Context, f Filter) ([]Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Key, 0, len(s.byID))
	for _, k := range s.byID {
		if f.Subject != "" && k.Subject != f.Subject {
			continue
		}
		if f.Tenant != "" && k.Tenant != f.Tenant {
			continue
		}
		out = append(out, cloneKey(k))
	}
	// UUIDv7 ids are time-ordered, so byte-descending is newest first.
	slices.SortFunc(out, func(a, b Key) int {
		return bytes.Compare(b.ID[:], a.ID[:])
	})
	return out, nil
}

func (s *memoryStore) Revoke(_ context.Context, keyID id.UUID, at time.Time) error {
	return s.update(keyID, func(k *Key) { k.RevokedAt = at })
}

func (s *memoryStore) Expire(_ context.Context, keyID id.UUID, at time.Time) error {
	return s.update(keyID, func(k *Key) { k.ExpiresAt = at })
}

func (s *memoryStore) Touch(_ context.Context, keyID id.UUID, at time.Time) error {
	return s.update(keyID, func(k *Key) { k.LastUsedAt = at })
}

func (s *memoryStore) update(keyID id.UUID, fn func(*Key)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.byID[keyID]
	if !ok {
		return ErrNotFound
	}
	fn(&k)
	s.byID[keyID] = k
	return nil
}
