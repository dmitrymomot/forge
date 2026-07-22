package invite

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Store persists Invite records. Implementations must be safe for
// concurrent use. Create fails with ErrDuplicate when a record with the
// same ID or Hash exists; lookups return ErrNotFound for unknown ids.
// Accept, Revoke, and Rotate are the single-use guards: each mutates only
// records still in the required state, atomically, and classifies the
// refusal otherwise — callers pre-check for better errors, but the store
// decision is authoritative under races.
type Store interface {
	Create(ctx context.Context, inv Invite) error
	Get(ctx context.Context, inviteID id.UUID) (Invite, error)
	// GetByHash is the accept path: one point lookup by the hex SHA-256 of
	// the presented plaintext token.
	GetByHash(ctx context.Context, hash string) (Invite, error)
	// List returns records matching f, newest first (UUIDv7 id order; ties
	// within one millisecond are unordered).
	List(ctx context.Context, f Filter) ([]Invite, error)
	// Accept atomically marks the invite accepted at `at` if it is still
	// pending (not accepted, not revoked, and expiring after at). It fails
	// with ErrNotFound, ErrAlreadyAccepted, ErrRevoked, or ErrExpired.
	Accept(ctx context.Context, inviteID id.UUID, at time.Time) error
	// Revoke atomically marks the invite revoked at `at` unless it was
	// accepted. Revoking an already-revoked invite is a no-op success that
	// keeps the original RevokedAt. It fails with ErrNotFound or
	// ErrAlreadyAccepted.
	Revoke(ctx context.Context, inviteID id.UUID, at time.Time) error
	// Rotate atomically swaps the token hash and expiry if the invite is
	// neither accepted nor revoked (an expired invite may be rotated —
	// that is what Resend is for). It fails with ErrNotFound,
	// ErrAlreadyAccepted, ErrRevoked, or ErrDuplicate (hash collision).
	Rotate(ctx context.Context, inviteID id.UUID, hash string, expiresAt time.Time) error
}
