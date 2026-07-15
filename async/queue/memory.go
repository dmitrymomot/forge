package queue

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// MemoryBroker is the built-in reference Broker: full semantics (leases,
// delayed jobs, dead-letter) over process memory. Use it for tests,
// dev/single-process apps, and as the behavioral reference for drivers.
// Jobs do not survive process restart.
type MemoryBroker struct {
	clk  clock.Clock
	jobs map[string]*memJob
	mu   sync.Mutex
}

type memJob struct {
	claimedUntil time.Time
	job          Job
	dead         bool
}

// MemoryOption configures NewMemoryBroker.
type MemoryOption func(*MemoryBroker)

// WithMemoryClock injects a clock (tests).
func WithMemoryClock(c clock.Clock) MemoryOption {
	return func(b *MemoryBroker) { b.clk = c }
}

// NewMemoryBroker builds an empty in-memory broker.
func NewMemoryBroker(opts ...MemoryOption) *MemoryBroker {
	b := &MemoryBroker{clk: clock.System(), jobs: make(map[string]*memJob)}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *MemoryBroker) Push(_ context.Context, job Job) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.jobs[job.ID] = &memJob{job: job}
	return nil
}

func (b *MemoryBroker) Claim(_ context.Context, queueName string, n int, lease time.Duration) ([]Job, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clk.Now()
	var due []*memJob
	for _, m := range b.jobs {
		if !m.dead && m.job.Queue == queueName && !m.job.RunAt.After(now) && m.claimedUntil.Before(now) {
			due = append(due, m)
		}
	}
	slices.SortFunc(due, func(a, c *memJob) int {
		if r := a.job.RunAt.Compare(c.job.RunAt); r != 0 {
			return r
		}
		return cmp.Compare(a.job.ID, c.job.ID)
	})
	if len(due) > n {
		due = due[:n]
	}
	out := make([]Job, 0, len(due))
	for _, m := range due {
		m.claimedUntil = now.Add(lease)
		m.job.Attempt++
		out = append(out, m.job)
	}
	return out, nil
}

func (b *MemoryBroker) Extend(_ context.Context, id string, lease time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	m.claimedUntil = b.clk.Now().Add(lease)
	return nil
}

func (b *MemoryBroker) Ack(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.jobs[id]; !ok {
		return ErrJobNotFound
	}
	delete(b.jobs, id)
	return nil
}

func (b *MemoryBroker) Nack(_ context.Context, id string, retryAt time.Time, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	m.job.RunAt = retryAt
	m.job.LastError = reason
	m.claimedUntil = time.Time{}
	return nil
}

func (b *MemoryBroker) Kill(_ context.Context, id string, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	m.dead = true
	m.job.LastError = reason
	m.claimedUntil = time.Time{}
	return nil
}

func (b *MemoryBroker) ListDead(_ context.Context, queueName string, limit int) ([]Job, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var dead []*memJob
	for _, m := range b.jobs {
		if m.dead && m.job.Queue == queueName {
			dead = append(dead, m)
		}
	}
	slices.SortFunc(dead, func(a, c *memJob) int {
		if r := a.job.CreatedAt.Compare(c.job.CreatedAt); r != 0 {
			return r
		}
		return cmp.Compare(a.job.ID, c.job.ID)
	})
	if len(dead) > limit {
		dead = dead[:limit]
	}
	out := make([]Job, 0, len(dead))
	for _, m := range dead {
		out = append(out, m.job)
	}
	return out, nil
}

func (b *MemoryBroker) Requeue(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if !m.dead {
		return ErrNotDead
	}
	m.dead = false
	m.job.Attempt = 0
	m.job.RunAt = b.clk.Now()
	m.claimedUntil = time.Time{}
	return nil
}

func (b *MemoryBroker) Purge(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if !m.dead {
		return ErrNotDead
	}
	delete(b.jobs, id)
	return nil
}

func (b *MemoryBroker) Stats(_ context.Context) (Stats, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := make(Stats)
	for _, m := range b.jobs {
		qs := st[m.job.Queue]
		if m.dead {
			qs.Dead++
		} else {
			qs.Pending++
		}
		st[m.job.Queue] = qs
	}
	return st, nil
}
