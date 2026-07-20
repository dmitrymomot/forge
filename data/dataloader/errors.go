package dataloader

import "errors"

var (
	// ErrNotFound resolves a key the BatchFunc did not return. Load wraps it
	// with the key; match with errors.Is.
	ErrNotFound = errors.New("dataloader: not found")

	// ErrFetchPanic wraps a panic recovered from the BatchFunc so waiters are
	// released with an error instead of deadlocking.
	ErrFetchPanic = errors.New("dataloader: fetch panicked")
)

// errNotConstructed reports use of a zero-value Loader that bypassed New.
var errNotConstructed = errors.New("dataloader: loader not constructed with New")

// notFoundError marks loader-generated per-key absence (it wraps ErrNotFound
// for consumers) so LoadMany can tell it apart from a caller batch error that
// happens to wrap ErrNotFound too.
type notFoundError struct{ err error }

func (e *notFoundError) Error() string { return e.err.Error() }
func (e *notFoundError) Unwrap() error { return e.err }
