package queue

import (
	"context"
	"encoding/json"
	"fmt"

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

func (c *Client) buildJob(ctx context.Context, name string, payload []byte, opts []PushOption) (Job, error) {
	p := pushConfig{queue: "default"}
	for _, opt := range opts {
		opt(&p)
	}
	scope := ""
	if c.scope != nil {
		s, err := c.scope(ctx)
		if err != nil {
			return Job{}, fmt.Errorf("%w: %w", ErrScopeMissing, err)
		}
		if s == "" {
			return Job{}, ErrScopeMissing
		}
		scope = s
	}
	now := c.clk.Now().UTC()
	runAt := now
	switch {
	case !p.runAt.IsZero():
		runAt = p.runAt.UTC()
	case p.delay > 0:
		runAt = now.Add(p.delay)
	}
	return Job{
		ID:          id.NewULID().String(),
		Queue:       p.queue,
		Type:        name,
		Payload:     payload,
		Scope:       scope,
		MaxAttempts: p.maxAttempts,
		RunAt:       runAt,
		CreatedAt:   now,
	}, nil
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
