package outbox

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/clock"
)

// MemoryStore is the in-process Store test double: the tx argument is
// ignored, so Add commits immediately even if the caller's transaction rolls
// back — no transactional semantics. Tests and throwaway dev only; production
// uses a transactional store (async/outbox/postgres) or brings its own.
type MemoryStore struct {
	clk     clock.Clock
	entries map[string]*memEntry
	mu      sync.Mutex
}

type memEntry struct {
	availableAt time.Time
	lastError   string
	job         queue.Job
	attempts    int
}

// MemoryOption configures NewMemoryStore.
type MemoryOption func(*MemoryStore)

// WithMemoryClock injects a clock (tests).
func WithMemoryClock(c clock.Clock) MemoryOption {
	return func(s *MemoryStore) { s.clk = c }
}

// NewMemoryStore builds an empty in-memory store.
func NewMemoryStore(opts ...MemoryOption) *MemoryStore {
	s := &MemoryStore{
		clk:     clock.System(),
		entries: make(map[string]*memEntry),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Add implements Store. The tx argument is ignored (see the type doc).
func (s *MemoryStore) Add(_ context.Context, _ any, jobs ...queue.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clk.Now().UTC()
	for _, j := range jobs {
		s.entries[j.ID] = &memEntry{job: j, availableAt: now}
	}
	return nil
}

// Claim implements Store.
func (s *MemoryStore) Claim(_ context.Context, n int, lease time.Duration) ([]Entry, error) {
	if n <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clk.Now().UTC()
	due := make([]*memEntry, 0, min(n, len(s.entries)))
	for _, e := range s.entries {
		if !e.availableAt.After(now) {
			due = append(due, e)
		}
	}
	slices.SortFunc(due, func(a, b *memEntry) int {
		if r := a.availableAt.Compare(b.availableAt); r != 0 {
			return r
		}
		return cmp.Compare(a.job.ID, b.job.ID)
	})
	if len(due) > n {
		due = due[:n]
	}
	claimed := make([]Entry, 0, len(due))
	for _, e := range due {
		e.availableAt = now.Add(lease)
		e.attempts++
		claimed = append(claimed, Entry{Job: e.job, Attempts: e.attempts, LastError: e.lastError})
	}
	slices.SortFunc(claimed, func(a, b Entry) int {
		if r := a.Job.CreatedAt.Compare(b.Job.CreatedAt); r != 0 {
			return r
		}
		return cmp.Compare(a.Job.ID, b.Job.ID)
	})
	return claimed, nil
}

// Delete implements Store. Unknown ids are ignored.
func (s *MemoryStore) Delete(_ context.Context, ids ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.entries, id)
	}
	return nil
}

// Fail implements Store. Unknown ids are ignored.
func (s *MemoryStore) Fail(_ context.Context, id string, retryAt time.Time, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok {
		e.availableAt = retryAt.UTC()
		e.lastError = reason
	}
	return nil
}

// Stats implements Store. Counts are exact.
func (s *MemoryStore) Stats(_ context.Context) (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Stats{Pending: len(s.entries)}
	for _, e := range s.entries {
		if st.Oldest.IsZero() || e.job.CreatedAt.Before(st.Oldest) {
			st.Oldest = e.job.CreatedAt
		}
	}
	return st, nil
}
