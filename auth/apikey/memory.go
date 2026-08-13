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

// MemoryStore is an in-memory record set for tests and development. Its
// methods have the effect signatures exactly, so a call site passes method
// values — mem.Save, mem.LoadByHash — with no adapter in between. State
// does not survive a restart.
type MemoryStore struct {
	byID   map[id.UUID]Key
	byHash map[string]id.UUID
	mu     sync.RWMutex
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:   make(map[id.UUID]Key),
		byHash: make(map[string]id.UUID),
	}
}

// cloneKey copies the record's reference fields so callers cannot mutate
// stored state through shared slices/maps (and vice versa). Nil Scopes and
// Meta are normalized to non-nil empty values — the read shape every
// backend should return, so callers see the same non-nil empties whichever
// storage backs the effects.
func cloneKey(k Key) Key {
	k.Scopes = slices.Clone(k.Scopes)
	if k.Scopes == nil {
		k.Scopes = []string{}
	}
	k.Meta = maps.Clone(k.Meta)
	if k.Meta == nil {
		k.Meta = map[string]string{}
	}
	return k
}

// Save implements SaveFunc.
func (s *MemoryStore) Save(_ context.Context, k Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(k)
}

func (s *MemoryStore) save(k Key) error {
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

// Load implements LoadFunc.
func (s *MemoryStore) Load(_ context.Context, keyID id.UUID) (Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.byID[keyID]
	if !ok {
		return Key{}, ErrNotFound
	}
	return cloneKey(k), nil
}

// LoadByHash implements LoadByHashFunc.
func (s *MemoryStore) LoadByHash(_ context.Context, hash string) (Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyID, ok := s.byHash[hash]
	if !ok {
		return Key{}, ErrNotFound
	}
	return cloneKey(s.byID[keyID]), nil
}

// List implements ListFunc.
func (s *MemoryStore) List(_ context.Context, f Filter) ([]Key, error) {
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
	sortNewestFirst(out)
	return out, nil
}

// sortNewestFirst orders by descending id bytes; UUIDv7 ids are
// time-ordered, so that is newest first.
func sortNewestFirst(keys []Key) {
	slices.SortFunc(keys, func(a, b Key) int {
		return bytes.Compare(b.ID[:], a.ID[:])
	})
}

// Revoke implements RevokeFunc.
func (s *MemoryStore) Revoke(_ context.Context, keyID id.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(keyID, func(k *Key) { k.RevokedAt = at })
}

// Expire sets a key's expiry. No operation needs it — Rotate expires the
// old key through Swap — but a backend owns the column, so tests and
// administrative tools can set it directly.
func (s *MemoryStore) Expire(_ context.Context, keyID id.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(keyID, func(k *Key) { k.ExpiresAt = at })
}

// Touch implements TouchFunc.
func (s *MemoryStore) Touch(_ context.Context, keyID id.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(keyID, func(k *Key) { k.LastUsedAt = at })
}

// Swap implements SwapFunc: both writes land under one lock, the in-memory
// stand-in for the single transaction a durable backend uses.
func (s *MemoryStore) Swap(_ context.Context, oldID id.UUID, oldExpiresAt time.Time, replacement Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[oldID]; !ok {
		return ErrNotFound
	}
	if err := s.save(replacement); err != nil {
		return err
	}
	return s.update(oldID, func(k *Key) { k.ExpiresAt = oldExpiresAt })
}

func (s *MemoryStore) update(keyID id.UUID, fn func(*Key)) error {
	k, ok := s.byID[keyID]
	if !ok {
		return ErrNotFound
	}
	fn(&k)
	s.byID[keyID] = k
	return nil
}
