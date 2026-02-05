package middlewares

import (
	"errors"
	"fmt"
	"time"
)

// PanicError represents a recovered panic.
type PanicError struct {
	Value any    // The panic value
	Stack []byte // Stack trace (nil if disabled)
}

// Error implements the error interface.
func (e *PanicError) Error() string {
	return fmt.Sprintf("panic: %v", e.Value)
}

// TimeoutError represents a request timeout.
type TimeoutError struct {
	Duration time.Duration // The timeout that was exceeded
}

// Error implements the error interface.
func (e *TimeoutError) Error() string {
	return fmt.Sprintf("request timeout after %s", e.Duration)
}

// IsPanicError returns true if the error is a PanicError.
func IsPanicError(err error) bool {
	var pe *PanicError
	return errors.As(err, &pe)
}

// IsTimeoutError returns true if the error is a TimeoutError.
func IsTimeoutError(err error) bool {
	var te *TimeoutError
	return errors.As(err, &te)
}

// AsPanicError extracts the PanicError from an error if present.
func AsPanicError(err error) (*PanicError, bool) {
	var pe *PanicError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}

// AsTimeoutError extracts the TimeoutError from an error if present.
func AsTimeoutError(err error) (*TimeoutError, bool) {
	var te *TimeoutError
	if errors.As(err, &te) {
		return te, true
	}
	return nil, false
}
