package session

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Record is the stored shape of a session: the first-class columns plus an
// opaque payload. Stores never interpret Payload and never see a Session.
type Record struct {
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastSeenAt  time.Time
	ElevatedAt  time.Time
	UserID      string
	IP          string
	UserAgent   string
	Fingerprint string
	Payload     []byte
	ID          id.UUID
	Remembered  bool
}

// Store is the minimum a backend must implement. Implementations must be safe
// for concurrent use and must never persist the raw token — key on Digest.
type Store interface {
	// Load returns the record for token, or ErrNotFound.
	Load(ctx context.Context, token string) (Record, error)
	// Save writes rec and returns the token the client should present next.
	// Server-side stores echo token back; a stateless store returns its fresh
	// encoding of rec, which is what lets both satisfy this interface.
	Save(ctx context.Context, token string, rec Record) (string, error)
	// Delete removes the record for token. Deleting an absent record is not an error.
	Delete(ctx context.Context, token string) error
}

// Toucher is the optional metadata-only refresh capability.
type Toucher interface {
	Touch(ctx context.Context, token string, lastSeenAt, expiresAt time.Time) error
}

// UserIndex is the optional per-user index behind device management.
type UserIndex interface {
	ListByUser(ctx context.Context, userID string) ([]Record, error)
	// DeleteByUser removes every record for userID except those in keep.
	DeleteByUser(ctx context.Context, userID string, keep ...id.UUID) error
	// DeleteOne removes sessionID only if it belongs to userID.
	DeleteOne(ctx context.Context, userID string, sessionID id.UUID) error
}

// Expirer is the optional bulk reaping capability. Stores whose backend expires
// records natively (a Mongo TTL index, a Redis key TTL) do not implement it.
type Expirer interface {
	DeleteExpired(ctx context.Context, now time.Time) (int, error)
}

// Digest maps a raw token to the value a store may persist. Drivers call this
// so the hashing rule lives in one place.
func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
