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
	_, ok := errors.AsType[*PanicError](err)
	return ok
}

// AsPanicError extracts the PanicError from an error if present.
func AsPanicError(err error) (*PanicError, bool) {
	return errors.AsType[*PanicError](err)
}
