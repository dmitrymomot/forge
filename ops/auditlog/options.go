package auditlog

import (
	"context"

	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	scope func(context.Context) (string, error)
	clock clock.Clock
	chain bool
}

func defaultConfig() config {
	return config{clock: clock.System()}
}

// Option configures New.
type Option func(*config)

// WithScope derives the tenant from context for every Record call and
// stamps it onto the event. Fail-closed: a hook error or empty tenant
// fails Record with ErrScope, and an event whose explicit Tenant
// disagrees with the hook fails with ErrTenantMismatch. A nil fn leaves
// the recorder unscoped — single-tenant applications pay no ceremony, and
// multi-tenant callers may still set Event.Tenant by hand.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithChain enables per-stream hash chaining: each event's Hash is a
// SHA-256 over its payload and the previous event's Hash, making the
// trail tamper-evident (stream = tenant; "" is the single-tenant stream).
// Chained writes serialize per stream, and the recorder must be the only
// writer of its streams — run one chained Recorder per stream, not one
// per replica. If the Sink implements ChainHead, the chain resumes from
// the persisted head on first write; otherwise every process restart
// starts a new chain.
func WithChain() Option {
	return func(c *config) { c.chain = true }
}

// WithClock sets the time source used to stamp Event.Time and generate
// IDs (default the system clock). A nil clock is ignored.
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clock = clk
		}
	}
}
