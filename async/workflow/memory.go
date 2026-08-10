package workflow

import (
	"bytes"
	"context"
	"errors"
	"sync"
)

// MemoryStore is the in-process Store: a mutex-guarded map. It survives
// nothing — a restart loses every checkpoint — so it fits tests, dev, and
// single-process apps whose runs are disposable; production consumers
// bring their own durable Store.
type MemoryStore struct {
	runs map[string]Run
	mu   sync.Mutex
}

// NewMemoryStore builds an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: make(map[string]Run)}
}

// Create implements Store.
func (s *MemoryStore) Create(_ context.Context, run Run) error {
	if run.ID == "" {
		return errors.New("workflow: Create requires a non-empty run id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; ok {
		return ErrRunAlreadyExists
	}
	run.State = bytes.Clone(run.State)
	s.runs[run.ID] = run
	return nil
}

// Get implements Store.
func (s *MemoryStore) Get(_ context.Context, id string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	run.State = bytes.Clone(run.State)
	return run, nil
}

// Delete implements Store.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[id]; !ok {
		return ErrRunNotFound
	}
	delete(s.runs, id)
	return nil
}

// Update implements Store.
func (s *MemoryStore) Update(_ context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.runs[run.ID]
	if !ok {
		return ErrRunNotFound
	}
	if stored.Version != run.Version {
		return ErrStaleRun
	}
	run.State = bytes.Clone(run.State)
	run.Version++
	s.runs[run.ID] = run
	return nil
}
