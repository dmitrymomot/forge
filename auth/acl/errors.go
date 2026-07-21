package acl

import "errors"

var (
	// ErrScope fails a Manager operation closed when the WithScope hook errors
	// or yields an empty tenant.
	ErrScope = errors.New("acl: scope hook failed")

	// ErrInvalidEntry rejects a write with an empty subject, resource type, or
	// action, or an entry whose Effect is neither Allow nor Deny.
	ErrInvalidEntry = errors.New("acl: invalid entry")
)
