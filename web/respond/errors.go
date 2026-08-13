package respond

import "errors"

var (
	// ErrNotFound is what Responder.NotFound raises. A missing row and a row the
	// reader may not see should both raise it, so nothing outside can tell them apart.
	ErrNotFound = errors.New("respond: not found")
	// ErrMethodNotAllowed is what Responder.MethodNotAllowed raises.
	ErrMethodNotAllowed = errors.New("respond: method not allowed")
	// ErrNoResponse is raised when a Handler returns no Response and no error, which
	// is a bug in the handler rather than a runtime condition.
	ErrNoResponse = errors.New("respond: handler returned no response")
	// ErrNoWriter is raised when a Raw response carries no write func.
	ErrNoWriter = errors.New("respond: raw response has no write func")
)
