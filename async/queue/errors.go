package queue

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidConfig is returned by Config.Validate and the constructors on bad configuration.
	ErrInvalidConfig = errors.New("queue: invalid config")
	// ErrNoHandler is recorded as the dead-letter reason when a claimed job's kind has no registered handler.
	ErrNoHandler = errors.New("queue: no handler registered for kind")
	// ErrJobNotFound is returned by DLQ operations when the id does not exist.
	ErrJobNotFound = errors.New("queue: job not found")
	// ErrScopeMissing is returned by Push when a scope hook is configured and yields an error or empty scope.
	ErrScopeMissing = errors.New("queue: scope missing")
	// ErrNotDead is returned by Requeue/Purge when the job exists but is not dead-lettered.
	ErrNotDead = errors.New("queue: job is not dead")
	// ErrTxUnsupported is returned by PushTx when the broker does not implement TxPusher.
	ErrTxUnsupported = errors.New("queue: broker does not support transactional push")
	// ErrLeaseLost is returned by the token-fenced broker ops (Extend, Ack,
	// Nack, Kill) when the token no longer owns the job: the lease expired and
	// another claim took over, or the job was already finalized. Fenced ops
	// never return ErrJobNotFound — an unknown id is indistinguishable from an
	// already-finalized one.
	ErrLeaseLost = errors.New("queue: lease lost")
)

// Cancel is a handler verdict: the job became moot; discard it as done
// without retrying and without dead-lettering. Named as a verdict value
// (handlers `return queue.Cancel`), not a conventional ErrFoo sentinel.
//
//nolint:staticcheck // ST1012: verdict value, not an error condition to match
var Cancel error = errors.New("queue: job cancelled")

type skipRetryError struct{ err error }

func (e *skipRetryError) Error() string { return fmt.Sprintf("queue: skip retry: %v", e.err) }
func (e *skipRetryError) Unwrap() error { return e.err }

// SkipRetry wraps err into a handler verdict: fail the job straight to the
// dead-letter queue without burning the remaining attempts (poison input).
// SkipRetry(nil) returns nil.
func SkipRetry(err error) error {
	if err == nil {
		return nil
	}
	return &skipRetryError{err: err}
}

// IsSkipRetry reports whether err carries the SkipRetry verdict.
func IsSkipRetry(err error) bool {
	var s *skipRetryError
	return errors.As(err, &s)
}
