package timeout

import "errors"

var (
	// ErrTimeout is passed to the responder when the request deadline expires
	// before the handler writes a response.
	ErrTimeout = errors.New("timeout: request deadline exceeded")
	// ErrInvalidConfig marks a non-positive Timeout.
	ErrInvalidConfig = errors.New("timeout: invalid config")
)
