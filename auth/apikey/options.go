package apikey

import (
	"context"
	"fmt"
	"time"
)

type config struct {
	scope         func(context.Context) (string, error)
	prefix        string
	errs          []error
	touchInterval time.Duration
	valid         bool
}

// Option configures New.
type Option func(*config)

// WithPrefix sets the key prefix (default "key"); keys read
// <prefix>_<payload><checksum>. The prefix must match [a-z0-9_]+.
// Environments are separate issuers: build one Manager with "sk_live" and
// another with "sk_test".
func WithPrefix(p string) Option {
	return func(c *config) {
		if !validPrefix(p) {
			c.errs = append(c.errs, fmt.Errorf("%w: invalid prefix %q", ErrConfig, p))
			return
		}
		c.prefix = p
	}
}

// WithScope derives the tenant from context for every management
// operation: Create stamps it, List is confined to it, and
// Get/Revoke/Rotate report ErrNotFound for other tenants' keys.
// Fail-closed: a hook error or empty tenant fails the operation with
// ErrScope. Verify is unaffected — the key record itself carries the
// tenant. A nil fn leaves the Manager unscoped.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithTouchInterval throttles last-used-at writes: Verify calls its
// TouchFunc only when LastUsedAt is staler than d (default 60s). Zero
// touches on every request; negative disables tracking.
func WithTouchInterval(d time.Duration) Option {
	return func(c *config) { c.touchInterval = d }
}
