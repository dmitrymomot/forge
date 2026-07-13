package oauthserver

import (
	"context"
	"sync"
)

// memoryStore is the in-process Store for tests and single-node dev.
type memoryStore struct {
	m  map[string]Client
	mu sync.Mutex
}

// NewMemoryStore returns an in-memory Store. Data is lost on restart; use
// oauthserver/pgstore in production.
func NewMemoryStore() Store {
	return &memoryStore{m: map[string]Client{}}
}

func (s *memoryStore) Create(_ context.Context, c Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[c.ID]; ok {
		return ErrDuplicateClient
	}
	s.m[c.ID] = c.clone()
	return nil
}

func (s *memoryStore) Get(_ context.Context, id string) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[id]
	if !ok {
		return Client{}, ErrClientNotFound
	}
	return c.clone(), nil
}

func (s *memoryStore) Update(_ context.Context, c Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[c.ID]; !ok {
		return ErrClientNotFound
	}
	s.m[c.ID] = c.clone()
	return nil
}

func (s *memoryStore) List(_ context.Context, tenantID string) ([]Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Client, 0, len(s.m))
	for _, c := range s.m {
		if tenantID == "" || c.TenantID == tenantID {
			out = append(out, c.clone())
		}
	}
	return out, nil
}

func (s *memoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}
