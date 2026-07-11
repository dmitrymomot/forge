package lock

import "errors"

// ErrNotHeld is returned when refreshing or releasing a lease this owner does
// not hold.
var ErrNotHeld = errors.New("lock: not held by this owner")

// ErrLockLost reports that a held lease was lost (a refresh failed); observe it
// via Lease.Done.
var ErrLockLost = errors.New("lock: lease lost")
