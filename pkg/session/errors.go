package session

import "errors"

// Session errors.
var (
	// ErrNotConfigured is returned when session functionality is used
	// but WithSession was not configured on the app.
	ErrNotConfigured = errors.New("session: not configured")

	// ErrNotFound is returned when a session does not exist.
	ErrNotFound = errors.New("session: not found")

	// ErrExpired is returned when a session has expired.
	ErrExpired = errors.New("session: expired")

	// ErrInvalidToken is returned when a session token is invalid.
	ErrInvalidToken = errors.New("session: invalid token")

	// ErrFingerprintMismatch is returned when session fingerprint validation fails.
	// This may indicate a session hijacking attempt.
	ErrFingerprintMismatch = errors.New("session: fingerprint mismatch")
)
