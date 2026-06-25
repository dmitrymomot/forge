package logger

import "errors"

// Sentinel errors returned (wrapped) by this package. Match with errors.Is.
var (
	// ErrInvalidConfig is returned for an invalid Config field or option value.
	ErrInvalidConfig = errors.New("logger: invalid config")
	// ErrOpenFile is returned when the log file (or its parent dirs) cannot be created.
	ErrOpenFile = errors.New("logger: open log file")
)
