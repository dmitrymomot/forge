package invite

import "errors"

var (
	// ErrNotFound is returned by a Store when no record matches, and by
	// management operations for other tenants' invites under WithScope (so
	// cross-tenant existence cannot be probed).
	ErrNotFound = errors.New("invite: record not found")

	// ErrDuplicate is returned by Store.Create and Store.Rotate when a
	// record with the same ID or Hash already exists.
	ErrDuplicate = errors.New("invite: duplicate record")

	// ErrMalformedToken rejects tokens failing prefix/length/checksum
	// validation — decided before any store access.
	ErrMalformedToken = errors.New("invite: malformed token")

	// ErrInviteNotFound rejects well-formed tokens with no matching record.
	ErrInviteNotFound = errors.New("invite: invite not found")

	// ErrRevoked rejects operations on revoked invites.
	ErrRevoked = errors.New("invite: invite revoked")

	// ErrAlreadyAccepted rejects operations on already-accepted invites —
	// including a second Accept, which is how single-use is surfaced.
	ErrAlreadyAccepted = errors.New("invite: invite already accepted")

	// ErrExpired rejects operations on expired invites.
	ErrExpired = errors.New("invite: invite expired")

	// ErrEmailRequired rejects CreateParams with an empty Email.
	ErrEmailRequired = errors.New("invite: email required")

	// ErrTenantMismatch rejects management calls whose explicit tenant
	// conflicts with the WithScope-derived tenant.
	ErrTenantMismatch = errors.New("invite: tenant mismatch")

	// ErrScope fails management operations closed when the WithScope hook
	// errors or yields an empty tenant.
	ErrScope = errors.New("invite: tenant scope unavailable")
)
