package httpserver

import "errors"

// Sentinel errors returned (often joined) by Run. Match with errors.Is.
var (
	// ErrNoHandler is returned by Run when the Server was constructed with a nil handler.
	ErrNoHandler = errors.New("httpserver: nil handler")
	// ErrInvalidConfig is returned (joined) by Run when an option or Config field has an invalid value.
	ErrInvalidConfig = errors.New("httpserver: invalid config")
	// ErrShutdownTimeout is returned by Run when the drain deadline was exceeded and connections were force-closed.
	ErrShutdownTimeout = errors.New("httpserver: graceful shutdown timed out")
)
