package shortlink

import "errors"

var (
	// ErrNotFound is returned by a Store when no record matches, by Resolve
	// for unknown codes, and by management operations for other tenants'
	// links under WithScope (so cross-tenant existence cannot be probed).
	ErrNotFound = errors.New("shortlink: link not found")

	// ErrDuplicate is returned by Store.Create when the code already exists,
	// and by Manager.Create when a requested vanity code is taken.
	ErrDuplicate = errors.New("shortlink: code already exists")

	// ErrCodeExhausted fails Create when every generation attempt collided
	// with an existing code — the code space is too dense for the configured
	// length; raise WithCodeLength.
	ErrCodeExhausted = errors.New("shortlink: code generation exhausted retries")

	// ErrInvalidCode rejects vanity codes failing charset or length
	// validation — decided before any store access.
	ErrInvalidCode = errors.New("shortlink: invalid code")

	// ErrReservedCode rejects vanity codes on the reserved-word blocklist.
	ErrReservedCode = errors.New("shortlink: reserved code")

	// ErrInvalidURL rejects destinations that do not parse as absolute URLs
	// with a host.
	ErrInvalidURL = errors.New("shortlink: invalid destination url")

	// ErrSchemeNotAllowed rejects destinations whose scheme is outside the
	// creation-time allowlist (default http and https).
	ErrSchemeNotAllowed = errors.New("shortlink: destination scheme not allowed")

	// ErrLinkExpired is returned by Resolve when the link's ExpiresAt has
	// passed. The record remains in the store.
	ErrLinkExpired = errors.New("shortlink: link expired")

	// ErrLinkDeactivated is returned by Resolve for deactivated links.
	ErrLinkDeactivated = errors.New("shortlink: link deactivated")

	// ErrTenantMismatch rejects management calls whose explicit tenant
	// conflicts with the WithScope-derived tenant.
	ErrTenantMismatch = errors.New("shortlink: tenant mismatch")

	// ErrScope fails management operations closed when the WithScope hook
	// errors or yields an empty tenant.
	ErrScope = errors.New("shortlink: tenant scope unavailable")
)
