package sqlite

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
//
// These are distinct from the Is… classification predicates (IsUniqueViolation and
// friends), which match the underlying *sqlite.Error result code.
var (
	// ErrInvalidConfig is returned (joined) by Validate and Open when an option or
	// Config field has an invalid value.
	ErrInvalidConfig = errors.New("sqlite: invalid config")
	// ErrConnect is returned by Open when a pool could not be built or the database
	// could not be reached.
	ErrConnect = errors.New("sqlite: connect failed")
	// ErrHealthcheck wraps a failed liveness ping from the Healthcheck closure.
	ErrHealthcheck = errors.New("sqlite: healthcheck failed")
)
