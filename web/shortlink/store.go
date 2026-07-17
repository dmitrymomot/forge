package shortlink

import (
	"context"
	"time"
)

// Store persists Link records. Implementations must be safe for concurrent
// use. Create fails with ErrDuplicate when the code already exists; lookups
// and mutators return ErrNotFound for unknown codes. Codes are matched
// case-sensitively (the generated alphabet is mixed-case).
//
// Mutators take a tenant predicate: with a non-empty tenant the mutation
// applies only when the record belongs to that tenant, reporting
// ErrNotFound otherwise; an empty tenant is unconstrained. The predicate
// must be enforced atomically with the mutation (one statement, not
// check-then-act) — codes are reusable after Delete and vanity codes are
// user-claimable, so a Manager-level pre-check could race a delete-and-
// recreate and mutate another tenant's link.
type Store interface {
	Create(ctx context.Context, l Link) error
	Get(ctx context.Context, code string) (Link, error)
	// List returns records matching f, newest first (CreatedAt descending,
	// code ascending on ties).
	List(ctx context.Context, f Filter) ([]Link, error)
	// Deactivate stamps DeactivatedAt with at. A zero at leaves the link
	// active (zero DeactivatedAt means active).
	Deactivate(ctx context.Context, code, tenant string, at time.Time) error
	Activate(ctx context.Context, code, tenant string) error
	Delete(ctx context.Context, code, tenant string) error
}

// Filter narrows List. Zero fields match everything.
type Filter struct {
	// Tenant confines the listing to one tenant; empty matches all.
	Tenant string
	// Limit caps the number of records returned; 0 means no cap.
	Limit int
}
