package apikey

import (
	"context"
	"time"
)

type config struct {
	scope         func(context.Context) (string, error)
	prefix        string
	touchInterval time.Duration
}

// Option configures New.
type Option func(*config)

// WithPrefix sets the key prefix (default "key"); keys read
// <prefix>_<payload><checksum>. The prefix must match [a-z0-9_]+.
// Environments are separate issuers: run one Manager with "sk_live" and
// another with "sk_test".
func WithPrefix(p string) Option {
	return func(c *config) { c.prefix = p }
}

// WithScope derives the tenant from context for every management
// operation: Create stamps it, List is confined to it, and
// Get/Revoke/Rotate report ErrNotFound for other tenants' keys.
// Fail-closed: a hook error or empty tenant fails the operation with
// ErrScope. Verify is unaffected — the key record itself carries the
// tenant. A nil fn leaves the manager unscoped.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}

// WithTouchInterval throttles last-used-at writes: Verify touches the
// record only when LastUsedAt is staler than d (default 60s). Zero
// touches on every request; negative disables tracking.
func WithTouchInterval(d time.Duration) Option {
	return func(c *config) { c.touchInterval = d }
}
