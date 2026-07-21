package session

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// MemoryStore is the built-in Store + UserIndex for tests and development.
// It grows unbounded (expired records are removed lazily on Load and via
// PurgeExpired) — production deployments use pgstore, cookiestore, or
// NewKVStore over a durable cache.Store.
type MemoryStore struct {
	byToken map[string]Record
	mu      sync.RWMutex
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byToken: make(map[string]Record)}
}

var (
	_ Store     = (*MemoryStore)(nil)
	_ UserIndex = (*MemoryStore)(nil)
)

// cloneRecord copies the record's reference fields so callers cannot mutate
// stored state through shared slices/maps (and vice versa).
func cloneRecord(rec Record) Record {
	rec.Data = bytes.Clone(rec.Data)
	rec.Fingerprint.Parts = maps.Clone(rec.Fingerprint.Parts)
	return rec
}

// Save upserts rec under token; the token is returned unchanged.
func (s *MemoryStore) Save(_ context.Context, token string, rec Record) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byToken[token] = cloneRecord(rec)
	return token, nil
}

// Load returns the record for token, or ErrNotFound.
func (s *MemoryStore) Load(_ context.Context, token string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.byToken[token]
	if !ok {
		return Record{}, ErrNotFound
	}
	return cloneRecord(rec), nil
}

// Delete removes the record for token; absent tokens are a no-op.
func (s *MemoryStore) Delete(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byToken, token)
	return nil
}

// ListByUser returns the records bound to userID within scope, newest first.
func (s *MemoryStore) ListByUser(_ context.Context, scope, userID string) ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Record{}
	for _, rec := range s.byToken {
		if rec.Scope == scope && rec.UserID == userID {
			out = append(out, cloneRecord(rec))
		}
	}
	// UUIDv7 ids are time-ordered, so byte-descending is newest first.
	slices.SortFunc(out, func(a, b Record) int { return bytes.Compare(b.ID[:], a.ID[:]) })
	return out, nil
}

// DeleteByUser removes every record bound to userID within scope, except the
// ids in keep.
func (s *MemoryStore) DeleteByUser(_ context.Context, scope, userID string, keep ...id.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	maps.DeleteFunc(s.byToken, func(_ string, rec Record) bool {
		return rec.Scope == scope && rec.UserID == userID && !slices.Contains(keep, rec.ID)
	})
	return nil
}

// DeleteOne removes the record for sessionID when it is bound to userID
// within scope; anything else is a no-op.
func (s *MemoryStore) DeleteOne(_ context.Context, scope, userID string, sessionID id.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	maps.DeleteFunc(s.byToken, func(_ string, rec Record) bool {
		return rec.Scope == scope && rec.UserID == userID && rec.ID == sessionID
	})
	return nil
}

// PurgeExpired removes records whose deadline is at or before now and reports
// how many were dropped. Long-lived dev processes call it periodically;
// tests usually don't need it.
func (s *MemoryStore) PurgeExpired(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.byToken)
	maps.DeleteFunc(s.byToken, func(_ string, rec Record) bool {
		return !rec.ExpiresAt.After(now)
	})
	return n - len(s.byToken)
}
