package invite

import (
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Invite is the stored invitation record. It never contains the plaintext
// token: Hash is the hex SHA-256 of the full plaintext, returned exactly
// once by Create and Resend.
type Invite struct {
	CreatedAt  time.Time
	ExpiresAt  time.Time // always set — invites expire
	AcceptedAt time.Time // zero = not accepted
	RevokedAt  time.Time // zero = not revoked
	Hash       string    // hex SHA-256 of the full plaintext token
	Email      string    // invitee address the token was issued for
	Tenant     string    // tenant being joined; empty in single-tenant apps
	Role       string    // opaque consumer payload — never interpreted here
	ID         id.UUID   // UUIDv7 record id — time-ordered, never secret-derived
}

// Pending reports whether the invite is still acceptable at now: not
// accepted, not revoked, not expired.
func (i Invite) Pending(now time.Time) bool {
	return i.AcceptedAt.IsZero() && i.RevokedAt.IsZero() && i.ExpiresAt.After(now)
}

// CreateParams describes an invite to mint. Email is required; Role is an
// opaque payload (a role name, a JSON blob — the package never reads it).
type CreateParams struct {
	Email  string // required
	Tenant string // optional; constrained by the WithScope hook when set
	Role   string // opaque; carried verbatim into the accept Claim
}

// Filter narrows List results. Zero fields match everything; Pending
// restricts to invites still acceptable at query time.
type Filter struct {
	Email   string
	Tenant  string
	Pending bool
}

// Claim is the verified result of accepting an invite: the tenant being
// joined, the email the token was addressed to, and the opaque role
// payload. Membership creation from a Claim is the consumer's job.
type Claim struct {
	Tenant string
	Email  string
	Role   string
	ID     id.UUID // accepted invite's record id, for audit trails
}
