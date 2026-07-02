package errorsx

import "errors"

// permanentError marks its wrapped error as non-retryable. Its unexported
// permanent() marker method is what IsPermanent detects via errors.As.
type permanentError struct {
	err error
}

func (e *permanentError) Error() string { return e.err.Error() }

func (e *permanentError) Unwrap() error { return e.err }

// permanent is the unexported marker method. It carries no data — its presence
// (found via errors.As on the permanenter interface) is the signal.
func (e *permanentError) permanent() {}

// permanenter is satisfied by a permanent-marked error. Kept unexported: callers
// use IsPermanent/IsRetryable, never the type or interface directly.
type permanenter interface{ permanent() }

// MarkPermanent tags err as non-retryable, wrapping it so codes and wrapped
// sentinels beneath it stay resolvable. MarkPermanent(nil) returns nil.
func MarkPermanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err (or anything in its chain) is marked permanent.
func IsPermanent(err error) bool {
	var p permanenter
	return errors.As(err, &p)
}

// IsRetryable is the negation of IsPermanent: an unmarked error is retryable.
func IsRetryable(err error) bool {
	return !IsPermanent(err)
}
