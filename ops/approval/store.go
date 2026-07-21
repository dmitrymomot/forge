package approval

import (
	"context"

	"github.com/dmitrymomot/forge/core/id"
)

// Store persists approval requests. Implementations must be safe for
// concurrent use.
//
// Update is the package's only concurrency primitive: every state
// transition is a compare-and-swap on Version. An implementation that does
// not enforce it atomically breaks dual control — two checkers voting
// concurrently could both read quorum-1 approvals and both write, losing a
// vote or counting one approver twice.
//
// Implementations may normalize nil and empty Decisions/Meta in either
// direction; callers must not depend on which form is returned. List must
// return a non-nil empty slice rather than nil when nothing matches.
//
// approvaltest.Run is the executable contract; every implementation must
// pass it.
type Store interface {
	// Create persists a new request. It returns ErrDuplicate when a
	// request with the same ID already exists.
	Create(ctx context.Context, r Request) error

	// Get loads one request. It returns ErrNotFound for unknown ids.
	Get(ctx context.Context, reqID id.UUID) (Request, error)

	// List returns requests matching f, newest first (UUIDv7 id order;
	// ties within one millisecond are unordered). A zero f.Limit defaults
	// to DefaultListLimit; every implementation must apply the same fixed
	// default.
	List(ctx context.Context, f Filter) ([]Request, error)

	// Update persists r only when the stored Version equals expect,
	// returning ErrConflict otherwise and ErrNotFound for unknown ids. The
	// implementation persists r with Version set to expect+1.
	Update(ctx context.Context, r Request, expect int64) error
}
