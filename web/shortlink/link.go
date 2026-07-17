package shortlink

import "time"

// Link is one short-code record. Code is the globally unique short code —
// short codes are URL path segments, so uniqueness cannot be per-tenant.
// Zero ExpiresAt means the link never expires; zero DeactivatedAt means it
// is active.
type Link struct {
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at,omitzero"`
	DeactivatedAt time.Time `json:"deactivated_at,omitzero"`
	Code          string    `json:"code"`
	URL           string    `json:"url"`
	Tenant        string    `json:"tenant,omitempty"`
}

// CreateParams describes a link to create.
type CreateParams struct {
	// ExpiresAt optionally expires the link; zero means never.
	ExpiresAt time.Time
	// URL is the destination. It must be an absolute URL with a host and a
	// scheme on the allowlist (default http and https) — destinations live
	// server-side, never in a query parameter.
	URL string
	// Code optionally requests a vanity code instead of a generated one:
	// 1–64 characters of [A-Za-z0-9_-], not on the reserved blocklist.
	Code string
	// Tenant optionally pins the owning tenant; constrained by the
	// WithScope hook when one is configured.
	Tenant string
}
