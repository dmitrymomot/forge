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
