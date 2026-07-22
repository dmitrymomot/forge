package invite

import (
	"context"
	"time"
)

type config struct {
	scope func(context.Context) (string, error)
	ttl   time.Duration
}

// Option configures New.
type Option func(*config)

// WithTTL sets how long a freshly minted (or resent) invite stays
// acceptable (default 7 days). New panics on a non-positive TTL.
func WithTTL(d time.Duration) Option {
	return func(c *config) { c.ttl = d }
}

// WithScope derives the tenant from context for every management
// operation: Create stamps it, List is confined to it, and
// Get/Revoke/Resend report ErrNotFound for other tenants' invites.
// Fail-closed: a hook error or empty tenant fails the operation with
// ErrScope. Accept and Peek are unaffected — the invitee typically has no
// tenant context yet, and the invite record itself carries the tenant. A
// nil fn leaves the manager unscoped.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}
