package health

import "errors"

// Sentinel errors for the health package.
var (
	// ErrCheckFailed is returned when one or more health checks fail.
	ErrCheckFailed = errors.New("health: check failed")

	// ErrCheckTimeout is returned when a health check exceeds its timeout.
	ErrCheckTimeout = errors.New("health: check timeout")
)
