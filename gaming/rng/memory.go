package rng

import (
	"context"
	"sync"
	"time"
)

type memoryStore struct {
	byID   map[string]Record
	active map[string]string // activeKey(scope, playerID) → record id
	mu     sync.Mutex
}

// NewMemoryStore returns an in-memory Store for tests and development.
func NewMemoryStore() Store {
	return &memoryStore{
		byID:   make(map[string]Record),
		active: make(map[string]string),
	}
}

func activeKey(scope, playerID string) string { return scope + "\x00" + playerID }

// cloneRecord isolates the caller from the stored ServerSeed slice.
func cloneRecord(r Record) Record {
	r.ServerSeed = append([]byte(nil), r.ServerSeed...)
	return r
}

func (m *memoryStore) Active(_ context.Context, scope, playerID string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.active[activeKey(scope, playerID)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return cloneRecord(m.byID[id]), nil
}

func (m *memoryStore) Create(_ context.Context, r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byID[r.ID]; exists {
		return ErrExists
	}
	key := activeKey(r.Scope, r.PlayerID)
	if r.Status == StatusActive {
		if _, exists := m.active[key]; exists {
			return ErrExists
		}
		m.active[key] = r.ID
	}
	m.byID[r.ID] = cloneRecord(r)
	return nil
}

func (m *memoryStore) ConsumeNonce(_ context.Context, scope, playerID string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.active[activeKey(scope, playerID)]
	if !ok {
		return Record{}, ErrNotFound
	}
	rec := m.byID[id]
	consumed := rec.Nonce
	rec.Nonce++
	m.byID[id] = rec
	rec.Nonce = consumed
	return cloneRecord(rec), nil
}

func (m *memoryStore) Reveal(_ context.Context, scope, id string, at time.Time) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.byID[id]
	if !ok || rec.Scope != scope {
		return Record{}, ErrNotFound
	}
	if rec.Status == StatusRevealed {
		return cloneRecord(rec), nil
	}
	rec.Status = StatusRevealed
	rec.RevealedAt = at
	delete(m.active, activeKey(scope, rec.PlayerID))
	m.byID[id] = rec
	return cloneRecord(rec), nil
}

func (m *memoryStore) Get(_ context.Context, scope, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.byID[id]
	if !ok || rec.Scope != scope {
		return Record{}, ErrNotFound
	}
	return cloneRecord(rec), nil
}
