package hostrouter

import "errors"

// Sentinel errors used as panic payloads by New (via WithHost / WithFallback) when
// a Router is misconfigured. Recover and match them with errors.Is.
var (
	// ErrNilHandler is used when a nil http.Handler is registered.
	ErrNilHandler = errors.New("hostrouter: nil handler")
	// ErrInvalidPattern is used when a host pattern is malformed.
	ErrInvalidPattern = errors.New("hostrouter: invalid host pattern")
	// ErrDuplicateHost is used when a host pattern is registered more than once.
	ErrDuplicateHost = errors.New("hostrouter: duplicate host pattern")
)
