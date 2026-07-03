package sentry

import "errors"

// Sentinel errors returned (wrapped) by this package. Match with errors.Is.
var (
	// ErrInvalidConfig is returned for an invalid Config field or option value.
	ErrInvalidConfig = errors.New("sentry: invalid config")
	// ErrSentryInit is returned when the Sentry SDK fails to initialize.
	ErrSentryInit = errors.New("sentry: initialization failed")
	// ErrSentryFlushTimeout is returned when buffered events are not flushed in time.
	ErrSentryFlushTimeout = errors.New("sentry: flush timed out")
)
