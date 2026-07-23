package session

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// MemoryStore is an in-process Store for tests and development. It implements
// every optional capability, so the conformance suite exercises all of them.
// It is safe for concurrent use and holds everything in memory: restarting the
// process logs everyone out.
type MemoryStore struct {
	byDigest map[string]Record
	mu       sync.RWMutex
}

var (
	_ Store     = (*MemoryStore)(nil)
	_ Toucher   = (*MemoryStore)(nil)
	_ UserIndex = (*MemoryStore)(nil)
	_ Expirer   = (*MemoryStore)(nil)
)

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byDigest: make(map[string]Record)}
}

// Load implements Store.
func (m *MemoryStore) Load(_ context.Context, token string) (Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.byDigest[Digest(token)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return cloneRecord(rec), nil
}

// Save implements Store. The token is echoed back: this is a server-side store,
// so the client's credential does not change unless the manager rotates it.
func (m *MemoryStore) Save(_ context.Context, token string, rec Record) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byDigest[Digest(token)] = cloneRecord(rec)
	return token, nil
}

// Delete implements Store.
func (m *MemoryStore) Delete(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byDigest, Digest(token))
	return nil
}

// Touch implements Toucher: metadata only, payload untouched.
func (m *MemoryStore) Touch(_ context.Context, token string, lastSeenAt, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := Digest(token)
	rec, ok := m.byDigest[d]
	if !ok {
		return ErrNotFound
	}
	rec.LastSeenAt = lastSeenAt
	rec.ExpiresAt = expiresAt
	m.byDigest[d] = rec
	return nil
}

// ListByUser implements UserIndex, newest first. tenant == "" matches any
// tenant, mirroring apikey's Filter.Tenant convention.
func (m *MemoryStore) ListByUser(_ context.Context, tenant, userID string) ([]Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Record
	for _, rec := range m.byDigest {
		if rec.UserID == userID && (tenant == "" || rec.Tenant == tenant) {
			out = append(out, cloneRecord(rec))
		}
	}
	slices.SortFunc(out, func(a, b Record) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

// DeleteByUser implements UserIndex, preserving the keep list. tenant == ""
// matches any tenant.
func (m *MemoryStore) DeleteByUser(_ context.Context, tenant, userID string, keep ...id.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for d, rec := range m.byDigest {
		if rec.UserID == userID && (tenant == "" || rec.Tenant == tenant) && !slices.Contains(keep, rec.ID) {
			delete(m.byDigest, d)
		}
	}
	return nil
}

// DeleteOne implements UserIndex. It removes every record carrying sessionID —
// rotation can leave more than one digest pointing at the same session — and
// only when the record belongs to tenant+userID. tenant == "" matches any
// tenant.
func (m *MemoryStore) DeleteOne(_ context.Context, tenant, userID string, sessionID id.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for d, rec := range m.byDigest {
		if rec.UserID == userID && rec.ID == sessionID && (tenant == "" || rec.Tenant == tenant) {
			delete(m.byDigest, d)
		}
	}
	return nil
}

// DeleteExpired implements Expirer.
func (m *MemoryStore) DeleteExpired(_ context.Context, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for d, rec := range m.byDigest {
		if !rec.ExpiresAt.IsZero() && !rec.ExpiresAt.After(now) {
			delete(m.byDigest, d)
			n++
		}
	}
	return n, nil
}

// cloneRecord copies the payload so a caller mutating the returned slice cannot
// corrupt the stored record.
func cloneRecord(rec Record) Record {
	rec.Payload = slices.Clone(rec.Payload)
	return rec
}
