package lockout

import "errors"

var (
	// ErrLocked reports that the identity is currently locked out. Only Do
	// returns it (wrapped in *LockedError); Allow and Fail report locked
	// state via Result instead.
	ErrLocked = errors.New("lockout: locked")
	// ErrFailedAttempt marks an authentication failure inside a Do callback:
	// return it (or an error wrapping it) to count the attempt. Any other
	// error passes through uncounted.
	ErrFailedAttempt = errors.New("lockout: failed attempt")
	// ErrScope reports a configured scope hook that failed or returned empty.
	ErrScope = errors.New("lockout: scope resolution failed")
	// ErrStore wraps failures of the underlying counter or lock stores.
	ErrStore = errors.New("lockout: store operation failed")
)

// LockedError carries lock details when Do rejects a locked identity or when
// a counted failure crosses the threshold. errors.Is(err, ErrLocked) always
// matches; when a failed attempt triggered the lock, the fn error chain
// (including ErrFailedAttempt) matches too.
type LockedError struct {
	Err    error // fn error that triggered the lock; nil when locked on entry
	Result Result
}

func (e *LockedError) Error() string {
	if e.Err != nil {
		return ErrLocked.Error() + ": " + e.Err.Error()
	}
	return ErrLocked.Error()
}

func (e *LockedError) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrLocked, e.Err}
	}
	return []error{ErrLocked}
}
