package auditlog

import (
	"context"
	"sync"
)

// MemorySink is an in-memory Sink for tests and development: it retains
// every event unboundedly and implements ChainHead, so chained recorders
// behave exactly as they do against a durable ChainHead-capable sink.
// Not for production.
type MemorySink struct {
	heads  map[string]string
	events []Event
	mu     sync.Mutex
}

var (
	_ Sink      = (*MemorySink)(nil)
	_ ChainHead = (*MemorySink)(nil)
)

// NewMemorySink returns an empty MemorySink.
func NewMemorySink() *MemorySink {
	return &MemorySink{heads: map[string]string{}}
}

// Write appends e.
func (s *MemorySink) Write(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	if e.Hash != "" {
		s.heads[e.Tenant] = e.Hash
	}
	return nil
}

// ChainHead returns the hash of the last chained event written for
// stream, or "" when none exists.
func (s *MemorySink) ChainHead(_ context.Context, stream string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heads[stream], nil
}

// Events returns a copy of every event written, in write order (non-nil
// even when empty).
func (s *MemorySink) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}
