package session

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

// Record is the storage form of one session. Data is the opaque JSON-encoded
// session payload; the Manager owns its typing. A zero Fingerprint (empty
// Hash) means hijack detection was off or no fingerprint was available when
// the session started. IP and UserAgent are display metadata for device
// listings, stamped by the request-level helpers.
type Record struct {
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastSeenAt  time.Time
	UserID      string
	Scope       string
	IP          string
	UserAgent   string
	Data        []byte
	Fingerprint fingerprint.Digest
	ID          id.UUID
}

// Store persists session records keyed by their bearer token.
// Implementations must be safe for concurrent use, must never persist the raw
// token (server-side stores key by a digest of it), and return ErrNotFound
// for unknown tokens.
type Store interface {
	// Save upserts rec under token and returns the token the client must be
	// handed: server-side stores persist rec and return token unchanged;
	// stateless stores (cookiestore) encode rec itself and return the fresh
	// encoding as the new token.
	Save(ctx context.Context, token string, rec Record) (string, error)
	Load(ctx context.Context, token string) (Record, error)
	// Delete revokes token. Deleting an absent token is a no-op.
	Delete(ctx context.Context, token string) error
}

// UserIndex is the optional Store extension backing multi-device management:
// device listings, per-device revocation, "log out other devices", and GDPR
// deletion. scope is the resolved tenancy scope ("" in single-tenant apps);
// implementations must filter by it so one tenant can never see or delete
// another's sessions. Stateless backings (cookiestore) and plain KV backings
// cannot implement it.
type UserIndex interface {
	// ListByUser returns the records bound to userID, newest first.
	ListByUser(ctx context.Context, scope, userID string) ([]Record, error)
	// DeleteByUser revokes every session bound to userID except the ids in
	// keep. Deleting a user with no sessions is a no-op.
	DeleteByUser(ctx context.Context, scope, userID string, keep ...id.UUID) error
	// DeleteOne revokes the single session sessionID bound to userID —
	// binding the delete to the user is what stops one user revoking
	// another's session by guessed id. Absent sessions are a no-op.
	DeleteOne(ctx context.Context, scope, userID string, sessionID id.UUID) error
}
