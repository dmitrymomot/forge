package clickhouse

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
// These are distinct from the classify predicates (IsTableNotFound and friends),
// which match the underlying *clickhouse.Exception rather than these sentinels.
var (
	// ErrInvalidConfig is returned (joined) by Validate and the constructors when an
	// option or Config field has an invalid value.
	ErrInvalidConfig = errors.New("clickhouse: invalid config")
	// ErrConnect is returned by Open/OpenDB when the DSN could not be parsed or the
	// server could not be reached within the configured retry budget.
	ErrConnect = errors.New("clickhouse: connect failed")
	// ErrHealthcheck wraps a failed liveness ping from a Healthcheck closure.
	ErrHealthcheck = errors.New("clickhouse: healthcheck failed")
)
