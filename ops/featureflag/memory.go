package featureflag

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// Memory is a mutable in-process Provider for runtime toggles (a guarded
// admin handler flipping a maintenance flag) and tests.
type Memory struct {
	flags Flags
	mu    sync.RWMutex
}

// NewMemory returns a Memory provider seeded with a deep copy of initial.
func NewMemory(initial Flags) *Memory {
	return &Memory{flags: initial.clone()}
}

// Flag implements Provider.
func (m *Memory) Flag(_ context.Context, key string) (Flag, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.flags[key]
	return f, ok, nil
}

// Set stores a validated flag; token slices are cloned.
func (m *Memory) Set(key string, f Flag) error {
	if key == "" {
		return ErrEmptyKey
	}
	if f.Rollout < 0 || f.Rollout > 100 {
		return fmt.Errorf("%w: got %d", ErrInvalidRollout, f.Rollout)
	}
	f.Allow = slices.Clone(f.Allow)
	f.Deny = slices.Clone(f.Deny)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flags[key] = f
	return nil
}

// Delete removes a flag; deleting a missing key is a no-op.
func (m *Memory) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.flags, key)
}

// All implements Lister; the result is a deep copy.
func (m *Memory) All(_ context.Context) (Flags, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.flags.clone(), nil
}
