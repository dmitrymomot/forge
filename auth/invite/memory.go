package invite

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

type memoryStore struct {
	byID   map[id.UUID]Invite
	byHash map[string]id.UUID
	mu     sync.RWMutex
}

// NewMemoryStore returns an in-memory Store for tests and development.
func NewMemoryStore() Store {
	return &memoryStore{
		byID:   make(map[id.UUID]Invite),
		byHash: make(map[string]id.UUID),
	}
}

func (s *memoryStore) Create(_ context.Context, inv Invite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[inv.ID]; ok {
		return ErrDuplicate
	}
	if _, ok := s.byHash[inv.Hash]; ok {
		return ErrDuplicate
	}
	s.byID[inv.ID] = inv
	s.byHash[inv.Hash] = inv.ID
	return nil
}

func (s *memoryStore) Get(_ context.Context, inviteID id.UUID) (Invite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inv, ok := s.byID[inviteID]
	if !ok {
		return Invite{}, ErrNotFound
	}
	return inv, nil
}

func (s *memoryStore) GetByHash(_ context.Context, hash string) (Invite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inviteID, ok := s.byHash[hash]
	if !ok {
		return Invite{}, ErrNotFound
	}
	return s.byID[inviteID], nil
}

func (s *memoryStore) List(_ context.Context, f Filter) ([]Invite, error) {
	now := time.Now().UTC()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Invite, 0, len(s.byID))
	for _, inv := range s.byID {
		if f.Email != "" && inv.Email != f.Email {
			continue
		}
		if f.Tenant != "" && inv.Tenant != f.Tenant {
			continue
		}
		if f.Pending && !inv.Pending(now) {
			continue
		}
		out = append(out, inv)
	}
	// UUIDv7 ids are time-ordered, so byte-descending is newest first.
	slices.SortFunc(out, func(a, b Invite) int {
		return bytes.Compare(b.ID[:], a.ID[:])
	})
	return out, nil
}

func (s *memoryStore) Accept(_ context.Context, inviteID id.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.byID[inviteID]
	switch {
	case !ok:
		return ErrNotFound
	case !inv.AcceptedAt.IsZero():
		return ErrAlreadyAccepted
	case !inv.RevokedAt.IsZero():
		return ErrRevoked
	case !inv.ExpiresAt.After(at):
		return ErrExpired
	}
	inv.AcceptedAt = at
	s.byID[inviteID] = inv
	return nil
}

func (s *memoryStore) Revoke(_ context.Context, inviteID id.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.byID[inviteID]
	switch {
	case !ok:
		return ErrNotFound
	case !inv.AcceptedAt.IsZero():
		return ErrAlreadyAccepted
	case !inv.RevokedAt.IsZero():
		return nil // idempotent — keep the original RevokedAt
	}
	inv.RevokedAt = at
	s.byID[inviteID] = inv
	return nil
}

func (s *memoryStore) Rotate(_ context.Context, inviteID id.UUID, hash string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.byID[inviteID]
	switch {
	case !ok:
		return ErrNotFound
	case !inv.AcceptedAt.IsZero():
		return ErrAlreadyAccepted
	case !inv.RevokedAt.IsZero():
		return ErrRevoked
	}
	if other, ok := s.byHash[hash]; ok && other != inviteID {
		return ErrDuplicate
	}
	delete(s.byHash, inv.Hash)
	inv.Hash = hash
	inv.ExpiresAt = expiresAt
	s.byID[inviteID] = inv
	s.byHash[hash] = inviteID
	return nil
}
