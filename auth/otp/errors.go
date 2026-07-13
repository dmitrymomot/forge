package otp

import "errors"

var (
	// ErrNotFound reports that no active code exists for the identifier —
	// never issued, expired, revoked, or already consumed.
	ErrNotFound = errors.New("otp: code not found")
	// ErrCodeMismatch reports a wrong code with attempts remaining.
	ErrCodeMismatch = errors.New("otp: code mismatch")
	// ErrTooManyAttempts reports a consumed attempt limit; the code is invalidated.
	ErrTooManyAttempts = errors.New("otp: too many attempts")
	// ErrScope reports a configured scope hook that failed or returned empty.
	ErrScope = errors.New("otp: scope resolution failed")
	// ErrStore wraps failures of the underlying cache.Store.
	ErrStore = errors.New("otp: store operation failed")
	// ErrInvalidConfig reports invalid constructor input.
	ErrInvalidConfig = errors.New("otp: invalid config")
)
