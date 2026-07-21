package session

import "errors"

var (
	// ErrNotFound reports that no session exists for the token — never
	// issued, destroyed, rotated away, or belonging to another scope.
	ErrNotFound = errors.New("session: not found")
	// ErrExpired reports a session past its idle or absolute deadline.
	ErrExpired = errors.New("session: expired")
	// ErrFingerprintMismatch reports a Strict-mode fingerprint drift; the
	// stored session is revoked before this is returned.
	ErrFingerprintMismatch = errors.New("session: fingerprint mismatch")
	// ErrNoUserIndex reports a user-level operation on a Store that does not
	// implement UserIndex (KV and cookie backings cannot list by user).
	ErrNoUserIndex = errors.New("session: store does not implement UserIndex")
	// ErrScope reports a configured scope hook that failed or returned empty.
	ErrScope = errors.New("session: scope resolution failed")
	// ErrStore wraps failures of the underlying Store.
	ErrStore = errors.New("session: store operation failed")
	// ErrCodec wraps session-data encode/decode failures.
	ErrCodec = errors.New("session: data encoding failed")
	// ErrInvalidConfig reports invalid constructor input.
	ErrInvalidConfig = errors.New("session: invalid config")
	// ErrInvalidInput reports an invalid method argument (empty user id).
	ErrInvalidInput = errors.New("session: invalid input")
)
