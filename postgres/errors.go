package postgres

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
//
// These are distinct from the Is… classification predicates (IsUniqueViolation and
// friends), which match the underlying *pgconn.PgError rather than these sentinels.
var (
	// ErrInvalidConfig is returned (joined) by Validate and Open when an option or
	// Config field has an invalid value.
	ErrInvalidConfig = errors.New("postgres: invalid config")
	// ErrConnect is returned by Open when the pool could not be built or the server
	// could not be reached within the configured retry budget.
	ErrConnect = errors.New("postgres: connect failed")
	// ErrHealthcheck wraps a failed liveness ping from the Healthcheck closure.
	ErrHealthcheck = errors.New("postgres: healthcheck failed")
)
