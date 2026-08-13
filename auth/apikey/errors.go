package apikey

import "errors"

var (
	// ErrNotFound is reported by a load effect when no record matches, and
	// by management operations for other tenants' keys under WithScope (so
	// cross-tenant existence cannot be probed).
	ErrNotFound = errors.New("apikey: record not found")

	// ErrDuplicate is reported by a SaveFunc when a record with the same ID
	// or Hash already exists.
	ErrDuplicate = errors.New("apikey: duplicate record")

	// ErrMalformedKey rejects credentials failing prefix/length/checksum
	// validation — decided before any store access.
	ErrMalformedKey = errors.New("apikey: malformed key")

	// ErrKeyNotFound rejects well-formed credentials with no matching record.
	ErrKeyNotFound = errors.New("apikey: key not found")

	// ErrKeyRevoked rejects credentials of revoked keys.
	ErrKeyRevoked = errors.New("apikey: key revoked")

	// ErrKeyExpired rejects credentials of expired keys.
	ErrKeyExpired = errors.New("apikey: key expired")

	// ErrSubjectRequired rejects CreateParams with an empty Subject.
	ErrSubjectRequired = errors.New("apikey: subject required")

	// ErrTenantMismatch rejects management calls whose explicit tenant
	// conflicts with the WithScope-derived tenant.
	ErrTenantMismatch = errors.New("apikey: tenant mismatch")

	// ErrScope fails management operations closed when the WithScope hook
	// errors or yields an empty tenant.
	ErrScope = errors.New("apikey: tenant scope unavailable")

	// ErrConfig rejects a Config that did not come from NewConfig, and is
	// returned by NewConfig itself for an invalid prefix.
	ErrConfig = errors.New("apikey: invalid config")

	// ErrNilEffect rejects an operation called without one of the storage
	// effects it performs. Only TouchFunc may be nil.
	ErrNilEffect = errors.New("apikey: nil effect func")
)
