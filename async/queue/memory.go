package queue

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

// MemoryBroker is the built-in reference Broker: full semantics (leases,
// fencing tokens, delayed jobs, dead-letter, retention) over process memory.
// Use it for tests, dev/single-process apps, and as the behavioral reference
// for drivers. Storage is bucketed per queue, so Claim cost scales with the
// claimed queue's live set, not the whole broker. Jobs do not survive process
// restart.
type MemoryBroker struct {
	clk    clock.Clock
	queues map[string]*memQueue
	index  map[string]string // job id → queue name, for O(1) id-addressed ops
	mu     sync.Mutex
}

type memQueue struct {
	live map[string]*memJob
	dead map[string]*memJob
}

type memJob struct {
	claimedUntil time.Time
	diedAt       time.Time
	token        string
	job          Job
}

// MemoryOption configures NewMemoryBroker.
type MemoryOption func(*MemoryBroker)

// WithMemoryClock injects a clock (tests).
func WithMemoryClock(c clock.Clock) MemoryOption {
	return func(b *MemoryBroker) { b.clk = c }
}

// NewMemoryBroker builds an empty in-memory broker.
func NewMemoryBroker(opts ...MemoryOption) *MemoryBroker {
	b := &MemoryBroker{
		clk:    clock.System(),
		queues: make(map[string]*memQueue),
		index:  make(map[string]string),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *MemoryBroker) bucket(q string) *memQueue {
	mq, ok := b.queues[q]
	if !ok {
		mq = &memQueue{live: make(map[string]*memJob), dead: make(map[string]*memJob)}
		b.queues[q] = mq
	}
	return mq
}

func (b *MemoryBroker) Push(_ context.Context, jobs ...Job) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, job := range jobs {
		b.bucket(job.Queue).live[job.ID] = &memJob{job: job}
		b.index[job.ID] = job.Queue
	}
	return nil
}

func (b *MemoryBroker) Claim(_ context.Context, queueName string, n int, lease time.Duration) ([]ClaimedJob, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	mq, ok := b.queues[queueName]
	if !ok {
		return nil, nil
	}
	now := b.clk.Now()
	due := make([]*memJob, 0, len(mq.live))
	for _, m := range mq.live {
		if !m.job.RunAt.After(now) && m.claimedUntil.Before(now) {
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
	token := id.NewUUID().String()
	out := make([]ClaimedJob, 0, len(due))
	for _, m := range due {
		m.claimedUntil = now.Add(lease)
		m.token = token
		m.job.Attempt++
		out = append(out, ClaimedJob{Job: m.job, Token: token})
	}
	return out, nil
}

// fenced returns the live job owned by token. A nil return means ErrLeaseLost
// for the caller: unknown id, dead job, cleared token, or token mismatch.
func (b *MemoryBroker) fenced(jobID, token string) *memJob {
	q, ok := b.index[jobID]
	if !ok {
		return nil
	}
	m, ok := b.queues[q].live[jobID]
	if !ok || token == "" || m.token != token {
		return nil
	}
	return m
}

func (b *MemoryBroker) Extend(_ context.Context, jobID, token string, lease time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.fenced(jobID, token)
	if m == nil {
		return ErrLeaseLost
	}
	m.claimedUntil = b.clk.Now().Add(lease)
	return nil
}

func (b *MemoryBroker) Ack(_ context.Context, jobID, token string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.fenced(jobID, token)
	if m == nil {
		return ErrLeaseLost
	}
	delete(b.queues[b.index[jobID]].live, jobID)
	delete(b.index, jobID)
	return nil
}

func (b *MemoryBroker) Nack(_ context.Context, jobID, token string, retryAt time.Time, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.fenced(jobID, token)
	if m == nil {
		return ErrLeaseLost
	}
	m.job.RunAt = retryAt
	m.job.LastError = reason
	m.claimedUntil = time.Time{}
	m.token = ""
	return nil
}

func (b *MemoryBroker) Kill(_ context.Context, jobID, token string, reason string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.fenced(jobID, token)
	if m == nil {
		return ErrLeaseLost
	}
	mq := b.queues[b.index[jobID]]
	delete(mq.live, jobID)
	m.job.LastError = reason
	m.claimedUntil = time.Time{}
	m.token = ""
	m.diedAt = b.clk.Now()
	mq.dead[jobID] = m
	return nil
}

func (b *MemoryBroker) ListDead(_ context.Context, queueName string, limit int) ([]Job, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	mq, ok := b.queues[queueName]
	if !ok {
		return nil, nil
	}
	dead := make([]*memJob, 0, len(mq.dead))
	for _, m := range mq.dead {
		dead = append(dead, m)
	}
	slices.SortFunc(dead, func(a, c *memJob) int {
		if r := a.diedAt.Compare(c.diedAt); r != 0 {
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

func (b *MemoryBroker) Requeue(_ context.Context, jobID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	q, ok := b.index[jobID]
	if !ok {
		return ErrJobNotFound
	}
	mq := b.queues[q]
	m, ok := mq.dead[jobID]
	if !ok {
		return ErrNotDead
	}
	delete(mq.dead, jobID)
	m.job.Attempt = 0
	m.job.RunAt = b.clk.Now()
	m.diedAt = time.Time{}
	mq.live[jobID] = m
	return nil
}

func (b *MemoryBroker) Purge(_ context.Context, jobID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	q, ok := b.index[jobID]
	if !ok {
		return ErrJobNotFound
	}
	mq := b.queues[q]
	if _, ok := mq.dead[jobID]; !ok {
		return ErrNotDead
	}
	delete(mq.dead, jobID)
	delete(b.index, jobID)
	return nil
}

func (b *MemoryBroker) PurgeDeadBefore(_ context.Context, cutoff time.Time) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, mq := range b.queues {
		for jobID, m := range mq.dead {
			if m.diedAt.Before(cutoff) {
				delete(mq.dead, jobID)
				delete(b.index, jobID)
				n++
			}
		}
	}
	return n, nil
}

func (b *MemoryBroker) Stats(_ context.Context) (Stats, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := make(Stats)
	for q, mq := range b.queues {
		if len(mq.live) == 0 && len(mq.dead) == 0 {
			continue
		}
		st[q] = QueueStats{Pending: len(mq.live), Dead: len(mq.dead)}
	}
	return st, nil
}
