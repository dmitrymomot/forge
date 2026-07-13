package lockout

import (
	"context"
	"errors"
	"fmt"
)

// Do wraps one authentication attempt: it rejects locked identities with a
// *LockedError before calling fn, counts failures fn reports, and resets the
// slate when fn succeeds.
//
// Classification: fn errors matching ErrFailedAttempt (errors.Is) are counted
// as failures; if the count crosses the threshold Do returns a *LockedError
// wrapping the fn error, otherwise the fn error passes through unchanged.
// Every other error passes through uncounted, so infrastructure failures can
// never lock a user out. A failed post-success Reset is returned as an
// ErrStore-wrapped error rather than swallowed — so a non-nil return is not
// always a denial: match *LockedError or ErrFailedAttempt to decide whether
// to reject the login, rather than treating any error as a failed attempt.
func (l *Locker) Do(ctx context.Context, key string, fn func(ctx context.Context) error) error {
	res, err := l.Allow(ctx, key)
	if err != nil {
		return err
	}
	if res.Locked {
		return &LockedError{Result: res}
	}

	err = fn(ctx)
	switch {
	case err == nil:
		return l.Reset(ctx, key)
	case errors.Is(err, ErrFailedAttempt):
		fres, ferr := l.Fail(ctx, key)
		if ferr != nil {
			return fmt.Errorf("%w: %w", err, ferr)
		}
		if fres.Locked {
			return &LockedError{Result: fres, Err: err}
		}
		return err
	default:
		return err
	}
}
