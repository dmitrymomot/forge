package eventrouter

import (
	"context"
	"fmt"
	"time"
)

// Option configures NewDestination.
type Option func(*Destination)

// WithBatchSize caps how many events one delivery carries; a filling batch
// flushes immediately. Size 1 disables batching: every event delivers alone,
// synchronously on its own job (the right setting for sync buses and
// per-event destinations like trackers). Panics on n < 1.
func WithBatchSize(n int) Option {
	if n < 1 {
		panic(fmt.Sprintf("eventrouter: WithBatchSize(%d): size must be >= 1", n))
	}
	return func(d *Destination) { d.maxSize = n }
}

// WithBatchAge caps how long a partial batch waits for more events before
// flushing — the egress latency floor under light traffic. Panics on a
// non-positive duration.
func WithBatchAge(age time.Duration) Option {
	if age <= 0 {
		panic(fmt.Sprintf("eventrouter: WithBatchAge(%v): age must be > 0", age))
	}
	return func(d *Destination) { d.maxAge = age }
}

// WithDeliveryTimeout bounds each Deliver call (the batch attempt and every
// poison-isolation re-delivery separately); expiry is a transient failure. 0
// disables the bound — the deliverer's own timeouts rule. Panics on a
// negative duration.
func WithDeliveryTimeout(timeout time.Duration) Option {
	if timeout < 0 {
		panic(fmt.Sprintf("eventrouter: WithDeliveryTimeout(%v): timeout must be >= 0 (0 disables it)", timeout))
	}
	return func(d *Destination) { d.timeout = timeout }
}

// WithScope installs the tenancy hook: called with each delivering job's
// context (restore the tenant there via queue.WithScopeContext on the
// eventbus Service), and the returned scope keys the destination's batches so
// they never mix tenants. Fail-closed: once configured, a hook error or empty
// scope fails the delivery with ErrScopeMissing instead of batching.
// Single-tenant apps do not configure it. Panics on a nil hook.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	if fn == nil {
		panic("eventrouter: WithScope(nil)")
	}
	return func(d *Destination) { d.scope = fn }
}
