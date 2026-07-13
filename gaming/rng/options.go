package rng

import (
	"context"

	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	scope    func(context.Context) (string, error)
	clock    clock.Clock
	scopeSet bool
	clockSet bool
}

// Option configures NewManager.
type Option func(*config)

// WithScope derives the tenant scope from context for every operation.
// Fail-closed: a hook error or empty scope fails the call with ErrNoScope.
// Seeds are always player-owned within a tenant — there is no global case.
// A nil fn is a constructor error.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope, c.scopeSet = fn, true }
}

// WithClock overrides the time source (tests). A nil clock is a
// constructor error.
func WithClock(cl clock.Clock) Option {
	return func(c *config) { c.clock, c.clockSet = cl, true }
}
