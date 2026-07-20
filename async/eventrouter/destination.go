package eventrouter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

// Destination is one external delivery target: a named Deliverer plus the
// batching policy in front of it. Construct once at startup and Route events
// into it; its name becomes the eventbus subscription name of every route, so
// it must be unique among the app's subscriptions. A Destination is safe for
// concurrent use and may be routed from any number of events — a batch mixes
// events, never tenants.
type Destination struct {
	deliverer Deliverer
	scope     func(ctx context.Context) (string, error)
	open      map[string]*batch // keyed by tenancy scope, "" without a hook
	name      string
	maxAge    time.Duration
	timeout   time.Duration
	maxSize   int
	mu        sync.Mutex
}

// batch is one open accumulation window. Joining jobs block on done; whoever
// detaches the batch from Destination.open (the size-filling join or the age
// timer, exactly one) flushes it and resolves every waiter's verdict.
type batch struct {
	ctx      context.Context // values of the first joiner, never cancelled
	timer    *time.Timer
	done     chan struct{}
	events   []Event
	verdicts []error
}

// NewDestination builds a Destination delivering through d. Defaults: batches
// of up to 100 events flushed after at most 1s, a 30s delivery timeout per
// flush, and no tenancy scoping. Panics on an empty name or nil deliverer —
// destinations are startup wiring, not runtime data.
func NewDestination(name string, d Deliverer, opts ...Option) *Destination {
	if name == "" {
		panic("eventrouter: NewDestination requires a non-empty name")
	}
	if d == nil {
		panic(fmt.Sprintf("eventrouter: NewDestination(%q) with nil deliverer", name))
	}
	dest := &Destination{
		name:      name,
		deliverer: d,
		open:      make(map[string]*batch),
		maxSize:   100,
		maxAge:    time.Second,
		timeout:   30 * time.Second,
	}
	for _, opt := range opts {
		opt(dest)
	}
	return dest
}

// Name returns the destination name — the eventbus subscription name of every
// route into it.
func (d *Destination) Name() string { return d.name }

// join adds e to the destination's open batch for the caller's scope and
// blocks until the batch flushes, returning this event's queue verdict. A
// cancelled ctx returns early with ctx.Err() — the event still rides the
// flush, and the retried job may deliver a duplicate receivers dedup.
func (d *Destination) join(ctx context.Context, e Event) error {
	scope := ""
	if d.scope != nil {
		s, err := d.scope(ctx)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrScopeMissing, err)
		}
		if s == "" {
			return ErrScopeMissing
		}
		scope = s
	}
	if d.maxSize == 1 {
		return verdict(d.deliver(ctx, []Event{e}))
	}
	d.mu.Lock()
	b, ok := d.open[scope]
	if !ok {
		b = &batch{
			ctx:    context.WithoutCancel(ctx),
			done:   make(chan struct{}),
			events: make([]Event, 0, d.maxSize),
		}
		b.timer = time.AfterFunc(d.maxAge, func() { d.flushExpired(scope, b) })
		d.open[scope] = b
	}
	b.events = append(b.events, e)
	idx := len(b.events) - 1
	full := len(b.events) >= d.maxSize
	if full {
		delete(d.open, scope)
	}
	d.mu.Unlock()
	if full {
		b.timer.Stop()
		d.flush(b)
	}
	select {
	case <-b.done:
		return b.verdicts[idx]
	case <-ctx.Done():
		return ctx.Err()
	}
}

// flushExpired is the age-timer path: it claims the batch unless a
// size-filling join already did, so exactly one flush runs per batch.
func (d *Destination) flushExpired(scope string, b *batch) {
	d.mu.Lock()
	if d.open[scope] != b {
		d.mu.Unlock()
		return
	}
	delete(d.open, scope)
	d.mu.Unlock()
	d.flush(b)
}

// flush delivers a detached batch and resolves every waiter's verdict: nil
// acknowledges, a transient error retries the whole batch, and a permanent
// error on a multi-event batch takes the poison-isolation path.
func (d *Destination) flush(b *batch) {
	b.verdicts = make([]error, len(b.events))
	err := d.deliver(b.ctx, b.events)
	switch {
	case err == nil:
	case errors.Is(err, ErrPermanent) && len(b.events) > 1:
		d.isolate(b)
	default:
		v := verdict(err)
		for i := range b.verdicts {
			b.verdicts[i] = v
		}
	}
	close(b.done)
}

// isolate re-delivers each event of a permanently rejected batch alone, so
// the poison event dead-letters by itself instead of taking its batchmates
// with it. Events the batch attempt already landed may be re-sent — receivers
// dedup by event ID; in-router suppression would risk silent loss instead.
func (d *Destination) isolate(b *batch) {
	for i := range b.events {
		b.verdicts[i] = verdict(d.deliver(b.ctx, b.events[i:i+1]))
	}
}

// deliver invokes the Deliverer under the per-flush timeout with panic
// recovery: a panicking deliverer is a failed (retryable) delivery, not a
// crashed worker or timer goroutine.
func (d *Destination) deliver(ctx context.Context, events []Event) (err error) {
	if d.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.timeout)
		defer cancel()
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("eventrouter: deliverer panic: %v", r)
		}
	}()
	return d.deliverer.Deliver(ctx, events)
}

// verdict maps a Deliverer outcome onto the queue's retry decision:
// permanent failures dead-letter without burning attempts, everything else
// retries.
func verdict(err error) error {
	if err != nil && errors.Is(err, ErrPermanent) {
		return queue.SkipRetry(err)
	}
	return err
}
