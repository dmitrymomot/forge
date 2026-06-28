package clock

import (
	"sync"
	"time"
)

// Clock reports the current time. Production code depends on this interface rather
// than calling time.Now directly, so time-dependent logic is deterministic in tests.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// System returns the production clock backed by time.Now.
func System() Clock { return systemClock{} }

// Mock is a goroutine-safe, controllable Clock for tests.
type Mock struct {
	t  time.Time
	mu sync.Mutex
}

// NewMock returns a Mock fixed at t.
func NewMock(t time.Time) *Mock { return &Mock{t: t} }

// Now returns the mock's current time.
func (m *Mock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.t
}

// Set replaces the mock's current time.
func (m *Mock) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t = t
}

// Advance moves the mock's current time forward by d.
func (m *Mock) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t = m.t.Add(d)
}
