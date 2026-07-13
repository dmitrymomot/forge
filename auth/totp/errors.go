package totp

import "errors"

var (
	// ErrInvalidCode reports a code that matches neither the TOTP window
	// nor any backup code.
	ErrInvalidCode = errors.New("totp: invalid code")
	// ErrReplayed reports a TOTP code at or before the last verified step —
	// or one lost to a concurrent verify of the same step.
	ErrReplayed = errors.New("totp: code already used")
	// ErrNotEnrolled reports an operation on a subject without a (confirmed,
	// where required) enrollment.
	ErrNotEnrolled = errors.New("totp: not enrolled")
	// ErrAlreadyEnrolled reports BeginEnroll on a confirmed enrollment;
	// Disable first to re-enroll.
	ErrAlreadyEnrolled = errors.New("totp: already enrolled")
	// ErrNotFound is the Store sentinel for an absent record.
	ErrNotFound = errors.New("totp: record not found")
	// ErrScope reports a failed or empty scope resolution (fail-closed),
	// or DisableTenant on an unscoped Manager.
	ErrScope = errors.New("totp: scope resolution failed")
)
