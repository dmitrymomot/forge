package otp

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	scope       func(context.Context) (string, error)
	clock       clock.Clock
	purpose     string
	ttl         time.Duration
	length      int
	maxAttempts int
}

// Option configures New.
type Option func(*config)

// WithPurpose isolates codes per flow (login, email-verify, password-reset).
// Two instances with different purposes never see each other's codes.
// Default "default".
func WithPurpose(p string) Option { return func(c *config) { c.purpose = p } }

// WithTTL sets the code lifetime. Default 10 minutes; must be positive.
func WithTTL(d time.Duration) Option { return func(c *config) { c.ttl = d } }

// WithLength sets the number of code digits. Default 6; valid range 4-10.
func WithLength(n int) Option { return func(c *config) { c.length = n } }

// WithMaxAttempts sets the per-code verify attempt limit. Default 5; valid
// range 1-255 (the counter is stored as a single byte).
func WithMaxAttempts(n int) Option { return func(c *config) { c.maxAttempts = n } }

// WithScope derives a tenant scope from the request context on every
// Generate, Verify, and Revoke call, so multi-tenant isolation is wired once
// at construction instead of at every call site. The hook fails closed: an
// error or empty scope aborts the operation with ErrScope — a code issued
// for one tenant can never resolve in another tenant's (or a global) bucket.
// Single-tenant applications omit this option.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithClock substitutes the time source used for expiry math. Default
// clock.System(). Tests use clock.Mock.
func WithClock(clk clock.Clock) Option { return func(c *config) { c.clock = clk } }
