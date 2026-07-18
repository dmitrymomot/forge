package smartlink

import (
	"context"
	"time"
)

// Store persists Link records by code. [MemoryStore] is the in-memory
// reference implementation; the pgstore subpackage is the Postgres driver
// implementing the same contract.
//
// Deactivate, Activate, and Delete take a tenant: when non-empty, the
// mutation applies only to a record owned by that tenant, and the tenant
// check must be atomic with the mutation (no separate read-then-write, since
// codes are reusable after Delete and a race could target the wrong
// record); a mismatched or missing code returns ErrNotFound either way. An
// empty tenant is unconstrained. Codes are case-sensitive.
type Store interface {
	// Create inserts l keyed by l.Code. Returns ErrDuplicate if the code
	// already exists.
	Create(ctx context.Context, l Link) error

	// Get returns the Link stored under code. Returns ErrNotFound if none exists.
	Get(ctx context.Context, code string) (Link, error)

	// List returns Links matching f, ordered by CreatedAt descending, then
	// Code ascending to break ties.
	List(ctx context.Context, f Filter) ([]Link, error)

	// Deactivate sets DeactivatedAt to at on code, scoped to tenant. at must
	// be non-zero — a zero DeactivatedAt means "active", so storing it would
	// silently reactivate — and implementations reject it with an error
	// wrapping ErrInvalidLink before touching the record.
	Deactivate(ctx context.Context, code, tenant string, at time.Time) error

	// Activate clears DeactivatedAt on code, scoped to tenant.
	Activate(ctx context.Context, code, tenant string) error

	// Delete removes code, scoped to tenant.
	Delete(ctx context.Context, code, tenant string) error
}
