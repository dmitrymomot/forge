package forgetest

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/dmitrymomot/forge"
)

// MemoryStore is an in-memory session store for testing.
// It implements forge.SessionStore backed by maps with a read-write mutex
// for safe concurrent access in parallel tests.
type MemoryStore struct {
	byID   map[string]*forge.Session
	byHash map[string]string // tokenHash -> sessionID
	mu     sync.RWMutex
}

// newMemoryStore creates a new in-memory session store.
func newMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:   make(map[string]*forge.Session),
		byHash: make(map[string]string),
	}
}

// Create persists a new session.
func (m *MemoryStore) Create(_ context.Context, s *forge.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.byID[s.ID] = copySession(s)
	m.byHash[s.TokenHash] = s.ID
	return nil
}

// GetByTokenHash retrieves a session by its SHA-256 token hash.
func (m *MemoryStore) GetByTokenHash(_ context.Context, tokenHash string) (*forge.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, ok := m.byHash[tokenHash]
	if !ok {
		return nil, forge.ErrSessionNotFound
	}

	s, ok := m.byID[id]
	if !ok {
		return nil, forge.ErrSessionNotFound
	}

	if time.Now().After(s.ExpiresAt) {
		return nil, forge.ErrSessionExpired
	}

	return copySession(s), nil
}

// Update saves changes to an existing session.
func (m *MemoryStore) Update(_ context.Context, s *forge.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old, ok := m.byID[s.ID]
	if !ok {
		return forge.ErrSessionNotFound
	}

	// Handle token rotation: if hash changed, update the index.
	if old.TokenHash != s.TokenHash {
		delete(m.byHash, old.TokenHash)
		m.byHash[s.TokenHash] = s.ID
	}

	m.byID[s.ID] = copySession(s)
	return nil
}

// Delete removes a session by its ID.
func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.byID[id]
	if ok {
		delete(m.byHash, s.TokenHash)
		delete(m.byID, id)
	}
	return nil
}

// ListByUserID retrieves all sessions for a user.
func (m *MemoryStore) ListByUserID(_ context.Context, userID string) ([]*forge.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*forge.Session
	for _, s := range m.byID {
		if s.UserID != nil && *s.UserID == userID {
			result = append(result, copySession(s))
		}
	}
	return result, nil
}

// CountByUserID returns the number of sessions for a user.
func (m *MemoryStore) CountByUserID(_ context.Context, userID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, s := range m.byID {
		if s.UserID != nil && *s.UserID == userID {
			count++
		}
	}
	return count, nil
}

// DeleteByUserID removes all sessions for a user.
func (m *MemoryStore) DeleteByUserID(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, s := range m.byID {
		if s.UserID != nil && *s.UserID == userID {
			delete(m.byHash, s.TokenHash)
			delete(m.byID, id)
		}
	}
	return nil
}

// DeleteByUserIDExcept removes all sessions for a user except the specified session ID.
func (m *MemoryStore) DeleteByUserIDExcept(_ context.Context, userID, exceptID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, s := range m.byID {
		if s.UserID != nil && *s.UserID == userID && id != exceptID {
			delete(m.byHash, s.TokenHash)
			delete(m.byID, id)
		}
	}
	return nil
}

// DeleteOldestByUserID removes the oldest session for a user (by LastActiveAt).
func (m *MemoryStore) DeleteOldestByUserID(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var oldestID string
	var oldestTime time.Time

	for id, s := range m.byID {
		if s.UserID != nil && *s.UserID == userID {
			if oldestID == "" || s.LastActiveAt.Before(oldestTime) {
				oldestID = id
				oldestTime = s.LastActiveAt
			}
		}
	}

	if oldestID != "" {
		if s, ok := m.byID[oldestID]; ok {
			delete(m.byHash, s.TokenHash)
		}
		delete(m.byID, oldestID)
	}
	return nil
}

// Touch updates the LastActiveAt timestamp.
func (m *MemoryStore) Touch(_ context.Context, id string, lastActiveAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.byID[id]
	if !ok {
		return forge.ErrSessionNotFound
	}
	s.LastActiveAt = lastActiveAt
	return nil
}

// GetByID retrieves a session by its ID. This is a test helper not part of the
// SessionStore interface, useful for inspecting session state in tests.
func (m *MemoryStore) GetByID(id string) (*forge.Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.byID[id]
	if !ok {
		return nil, false
	}
	return copySession(s), true
}

// Count returns the total number of sessions in the store.
func (m *MemoryStore) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byID)
}

// copySession deep-copies a session to prevent data races in parallel tests.
func copySession(s *forge.Session) *forge.Session {
	cp := *s

	if s.UserID != nil {
		uid := *s.UserID
		cp.UserID = &uid
	}

	if s.Data != nil {
		cp.Data = make(map[string]any, len(s.Data))
		maps.Copy(cp.Data, s.Data)
	}

	return &cp
}
