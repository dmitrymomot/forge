package supervisor

import "errors"

// Sentinel errors returned (often joined) by Run. Match with errors.Is.
var (
	// ErrNoServices is returned by Run when no services were registered.
	ErrNoServices = errors.New("supervisor: no services registered")
	// ErrUnnamedService is returned by Run when a registered service has an empty Name.
	ErrUnnamedService = errors.New("supervisor: service has empty name")
	// ErrShutdownTimeout is returned by Run when services do not stop within the grace timeout.
	ErrShutdownTimeout = errors.New("supervisor: graceful shutdown timed out")
	// ErrPanic wraps a value recovered from a panicking service's Run.
	ErrPanic = errors.New("supervisor: service panicked")
)
