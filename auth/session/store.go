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
// Tenant is the owning tenant scope, stamped by a configured WithScope hook;
// it is empty in single-tenant apps and never composed into the token or
// digest.
type Record struct {
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastSeenAt  time.Time
	ElevatedAt  time.Time
	UserID      string
	Tenant      string
	IP          string
	UserAgent   string
	Fingerprint string
	Payload     []byte
	ID          id.UUID
	Remembered  bool
}

// Store is the minimum a backend must implement. Implementations must be safe
// for concurrent use and must never persist the raw token — key on Digest.
// Save must persist Record.Tenant and Load must return it, so a configured
// scope hook's post-filter in Manager.Load can compare against it.
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

// UserIndex is the optional per-user index behind device management. Every
// method takes a tenant: "" means no tenant constraint (single-tenant, or an
// unscoped manager); a non-empty tenant confines the operation so one tenant's
// device-management call can never read or delete another tenant's sessions for
// the same user id.
type UserIndex interface {
	ListByUser(ctx context.Context, tenant, userID string) ([]Record, error)
	// DeleteByUser removes every record for tenant+userID except those in keep.
	DeleteByUser(ctx context.Context, tenant, userID string, keep ...id.UUID) error
	// DeleteOne removes sessionID only if it belongs to tenant+userID.
	DeleteOne(ctx context.Context, tenant, userID string, sessionID id.UUID) error
}

// Expirer is the optional bulk reaping capability. Stores whose backend expires
// records natively (a Mongo TTL index, a Redis key TTL) do not implement it.
//
// DeleteExpired removes every record whose ExpiresAt is at or before now — the
// boundary is inclusive, so a record expiring exactly at now is reaped. A zero
// ExpiresAt means the record never expires and must never be reaped.
type Expirer interface {
	DeleteExpired(ctx context.Context, now time.Time) (int, error)
}

// Digest maps a raw token to the value a store may persist. Drivers call this
// so the hashing rule lives in one place.
func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
