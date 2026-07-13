package apikey

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Store persists Key records. Implementations must be safe for concurrent
// use. Create fails with ErrDuplicate when a record with the same ID or
// Hash exists; lookups and mutators return ErrNotFound for unknown ids.
// Implementations may normalize nil and empty Scopes/Meta (and a nil vs.
// empty List result) in either direction; callers must not depend on
// which form is returned.
type Store interface {
	Create(ctx context.Context, k Key) error
	Get(ctx context.Context, keyID id.UUID) (Key, error)
	// GetByHash is the verification path: one point lookup by the hex
	// SHA-256 of the presented plaintext.
	GetByHash(ctx context.Context, hash string) (Key, error)
	// List returns records matching f, newest first (UUIDv7 id order;
	// ties within one millisecond are unordered).
	List(ctx context.Context, f Filter) ([]Key, error)
	Revoke(ctx context.Context, keyID id.UUID, at time.Time) error
	Expire(ctx context.Context, keyID id.UUID, at time.Time) error
	Touch(ctx context.Context, keyID id.UUID, at time.Time) error
}
