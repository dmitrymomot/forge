package hostrouter

import "errors"

var (
	// ErrNilHandler is returned (joined) by New when a nil http.Handler is registered.
	ErrNilHandler = errors.New("hostrouter: nil handler")
	// ErrInvalidPattern is returned (joined) by New when a host pattern is malformed.
	ErrInvalidPattern = errors.New("hostrouter: invalid host pattern")
	// ErrDuplicateHost is returned (joined) by New when a host pattern is registered
	// more than once.
	ErrDuplicateHost = errors.New("hostrouter: duplicate host pattern")
	// ErrNilLookup is returned (joined) by New when WithLookup is given a nil LookupFunc.
	ErrNilLookup = errors.New("hostrouter: nil lookup")
	// ErrHostNotFound tells the Router that the LookupFunc knows no such host, so the
	// request reaches the fallback. A LookupFunc returns it in place of its store's
	// no-rows error.
	ErrHostNotFound = errors.New("hostrouter: host not found")
)
