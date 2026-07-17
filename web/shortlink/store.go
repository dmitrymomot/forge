package shortlink

import (
	"context"
	"time"
)

// Store persists Link records. Implementations must be safe for concurrent
// use. Create fails with ErrDuplicate when the code already exists; lookups
// and mutators return ErrNotFound for unknown codes. Codes are matched
// case-sensitively (the generated alphabet is mixed-case).
type Store interface {
	Create(ctx context.Context, l Link) error
	Get(ctx context.Context, code string) (Link, error)
	// List returns records matching f, newest first (CreatedAt descending,
	// code ascending on ties).
	List(ctx context.Context, f Filter) ([]Link, error)
	Deactivate(ctx context.Context, code string, at time.Time) error
	Activate(ctx context.Context, code string) error
	Delete(ctx context.Context, code string) error
}

// Filter narrows List. Zero fields match everything.
type Filter struct {
	Tenant string
}
