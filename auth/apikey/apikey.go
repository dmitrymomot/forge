package apikey

import (
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Key is the stored API-key record. It never contains the plaintext
// secret: Hash is the hex SHA-256 of the full plaintext and Preview its
// first 12 characters for dashboard display.
type Key struct {
	CreatedAt  time.Time
	ExpiresAt  time.Time         // zero = never expires
	LastUsedAt time.Time         // zero = never used
	RevokedAt  time.Time         // zero = active
	Meta       map[string]string // caller extras, copied into Identity.Meta on verify
	Hash       string            // hex SHA-256 of the full plaintext key
	Preview    string            // first 12 plaintext chars — safe to display
	Name       string            // human label
	Subject    string            // principal the key acts as — never empty
	Tenant     string            // owning tenant; empty in single-tenant apps
	Scopes     []string          // carried into Identity.Scopes; never enforced here
	ID         id.UUID           // UUIDv7 record id — time-ordered, never secret-derived
}

// CreateParams describes a key to mint. Subject is required: for personal
// keys it is the user id; for tenant-wide keys, whatever principal
// represents the org acting as itself (tenant id or a service-account id).
type CreateParams struct {
	ExpiresAt time.Time // zero = never
	Meta      map[string]string
	Name      string
	Subject   string // required
	Tenant    string // optional; constrained by the WithScope hook when set
	Scopes    []string
}

// Filter narrows List results. Zero fields match everything.
type Filter struct {
	Subject string
	Tenant  string
}
