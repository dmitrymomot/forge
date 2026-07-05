package circuitbreaker

import (
	"errors"
	"time"
)

// ErrOpen is the sentinel a rejected call matches with errors.Is. The concrete
// error returned by Do also reports a suggested retry delay via RetryAfter.
var ErrOpen = errors.New("circuitbreaker: circuit open")

// openError is returned when the breaker rejects a call. It unwraps to ErrOpen
// (so errors.Is(err, ErrOpen) holds) and reports the delay until the next
// probe, satisfying retry.RetryAfterError and feeding the HTTP middleware's
// Retry-After header.
type openError struct{ retryAfter time.Duration }

func (e *openError) Error() string             { return ErrOpen.Error() }
func (e *openError) Unwrap() error             { return ErrOpen }
func (e *openError) RetryAfter() time.Duration { return e.retryAfter }
