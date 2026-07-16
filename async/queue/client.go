package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

// Client produces jobs. It is cheap, safe for concurrent use, and shared
// app-wide. Wire the same Broker instance into the worker Service.
type Client struct {
	broker Broker
	scope  func(ctx context.Context) (string, error)
	clk    clock.Clock
}

// NewClient builds a producer over broker.
func NewClient(broker Broker, opts ...ClientOption) *Client {
	c := &Client{broker: broker, clk: clock.System()}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Push enqueues a typed job. The payload is marshaled to JSON; the kind binds
// the job name to the payload type at compile time.
func Push[T any](ctx context.Context, c *Client, k Kind[T], payload T, opts ...PushOption) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("queue: marshal payload for %q: %w", k.Name(), err)
	}
	job, err := c.buildJob(ctx, k.Name(), raw, opts)
	if err != nil {
		return err
	}
	return c.broker.Push(ctx, job)
}

// PushTx enqueues a typed job inside a caller-owned database transaction. The
// broker must implement TxPusher (pgqueue does); otherwise ErrTxUnsupported.
func PushTx[T any](ctx context.Context, c *Client, tx any, k Kind[T], payload T, opts ...PushOption) error {
	tp, ok := c.broker.(TxPusher)
	if !ok {
		return ErrTxUnsupported
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("queue: marshal payload for %q: %w", k.Name(), err)
	}
	job, err := c.buildJob(ctx, k.Name(), raw, opts)
	if err != nil {
		return err
	}
	return tp.PushTx(ctx, tx, job)
}

// PushRaw enqueues a job by name with a caller-encoded JSON payload — the
// escape hatch for enqueuing kinds this codebase does not import. Prefer Push.
func (c *Client) PushRaw(ctx context.Context, name string, payload json.RawMessage, opts ...PushOption) error {
	if name == "" {
		return fmt.Errorf("queue: push raw: empty kind name")
	}
	if !json.Valid(payload) {
		return fmt.Errorf("queue: push raw %q: payload is not valid JSON", name)
	}
	job, err := c.buildJob(ctx, name, payload, opts)
	if err != nil {
		return err
	}
	return c.broker.Push(ctx, job)
}

// pushBase is the per-push-call state shared by every job in a batch: parsed
// options, resolved scope, and the batch timestamp.
type pushBase struct {
	now   time.Time
	scope string
	p     pushConfig
}

func (c *Client) pushBase(ctx context.Context, opts []PushOption) (pushBase, error) {
	p := pushConfig{queue: "default"}
	for _, opt := range opts {
		opt(&p)
	}
	if p.queue == "" {
		return pushBase{}, fmt.Errorf("queue: push: empty queue name")
	}
	scope := ""
	if c.scope != nil {
		s, err := c.scope(ctx)
		if err != nil {
			return pushBase{}, fmt.Errorf("%w: %w", ErrScopeMissing, err)
		}
		if s == "" {
			return pushBase{}, ErrScopeMissing
		}
		scope = s
	}
	return pushBase{p: p, scope: scope, now: c.clk.Now().UTC()}, nil
}

func (base pushBase) job(name string, payload []byte) Job {
	runAt := base.now
	switch {
	case !base.p.runAt.IsZero():
		runAt = base.p.runAt.UTC()
	case base.p.delay > 0:
		runAt = base.now.Add(base.p.delay)
	}
	return Job{
		ID:          id.NewUUID().String(),
		Queue:       base.p.queue,
		Type:        name,
		Payload:     payload,
		Scope:       base.scope,
		MaxAttempts: base.p.maxAttempts,
		RunAt:       runAt,
		CreatedAt:   base.now,
	}
}

func (c *Client) buildJob(ctx context.Context, name string, payload []byte, opts []PushOption) (Job, error) {
	base, err := c.pushBase(ctx, opts)
	if err != nil {
		return Job{}, err
	}
	return base.job(name, payload), nil
}

// PushMany enqueues one typed job per payload in a single atomic batch: one
// scope resolution, one option parse, one broker round trip. An empty slice
// is a no-op.
func PushMany[T any](ctx context.Context, c *Client, k Kind[T], payloads []T, opts ...PushOption) error {
	if len(payloads) == 0 {
		return nil
	}
	base, err := c.pushBase(ctx, opts)
	if err != nil {
		return err
	}
	jobs := make([]Job, 0, len(payloads))
	for i, p := range payloads {
		raw, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("queue: marshal payload %d for %q: %w", i, k.Name(), err)
		}
		jobs = append(jobs, base.job(k.Name(), raw))
	}
	return c.broker.Push(ctx, jobs...)
}

// ListDead returns up to limit dead-lettered jobs in the queue.
func (c *Client) ListDead(ctx context.Context, queue string, limit int) ([]Job, error) {
	return c.broker.ListDead(ctx, queue, limit)
}

// Requeue returns a dead job to pending with its attempt budget reset.
func (c *Client) Requeue(ctx context.Context, jobID string) error {
	return c.broker.Requeue(ctx, jobID)
}

// Purge permanently deletes a dead job.
func (c *Client) Purge(ctx context.Context, jobID string) error {
	return c.broker.Purge(ctx, jobID)
}

// Stats reports pending/dead counts per queue (health checks, dashboards).
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	return c.broker.Stats(ctx)
}
