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
// Create and Update must persist Record.Tenant and Load must return it, so a
// configured scope hook's post-filter in Manager.Load can compare against it.
//
// Create and Update are deliberately not one upsert: Update failing on an
// absent record is what makes revocation terminal. A request that loaded a
// session, lost a race with Destroy/Revoke, and then commits its stale
// snapshot must get ErrNotFound — an upsert would silently resurrect the
// revoked record with a fresh deadline.
//
// A stateless store (one that encodes the record into the credential itself)
// cannot check existence server-side; it re-encodes on both calls and its
// revocation guarantee is only as strong as the credential's own expiry.
type Store interface {
	// Load returns the record for token, or ErrNotFound.
	Load(ctx context.Context, token string) (Record, error)
	// Create writes rec under a token no record exists for, returning the token
	// the client should present next. Server-side stores insert and echo token
	// back, and must fail (ErrExists) rather than overwrite if a record is
	// already stored under it; a stateless store returns its fresh encoding of
	// rec.
	Create(ctx context.Context, token string, rec Record) (string, error)
	// Update rewrites the existing record for token, returning the token the
	// client should present next. It must return ErrNotFound when no record
	// exists — never insert — so a stale snapshot cannot recreate a deleted
	// record.
	Update(ctx context.Context, token string, rec Record) (string, error)
	// Delete removes the record for token. Deleting an absent record is not an error.
	Delete(ctx context.Context, token string) error
}

// Toucher is the optional metadata-only refresh capability. Touch returns
// ErrNotFound for an absent record and must be monotonic: a timestamp at or
// before the stored one is dropped, never written, so two racing requests
// cannot move LastSeenAt or ExpiresAt backward.
type Toucher interface {
	Touch(ctx context.Context, token string, lastSeenAt, expiresAt time.Time) error
}

// UserIndex is the optional per-user index behind device management. Every
// method takes a tenant: "" means no tenant constraint (single-tenant, or an
// unscoped manager); a non-empty tenant confines the operation so one tenant's
// device-management call can never read or delete another tenant's sessions for
// the same user id.
//
// Driver note: "" is a single-tenant wildcard, not "tenant scoping disabled". A
// scoped manager resolves a non-empty tenant on every call and fails closed on
// an empty one, so a driver only ever receives "" from a single-tenant caller —
// where matching any row is correct because every row's Tenant is also "".
type UserIndex interface {
	// ListByUser may include expired records that no reaper has removed yet;
	// Manager.ListByUser filters them, so drivers need no expiry predicate.
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
