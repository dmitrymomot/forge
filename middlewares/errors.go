package middlewares

import (
	"errors"
	"fmt"
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

// IsPanicError returns true if the error is a PanicError.
func IsPanicError(err error) bool {
	var pe *PanicError
	return errors.As(err, &pe)
}

// AsPanicError extracts the PanicError from an error if present.
func AsPanicError(err error) (*PanicError, bool) {
	var pe *PanicError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
