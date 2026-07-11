package lock

import (
	"context"
	"time"
)

// Store is the 3-method distributed-lease seam. Implementations must make
// Acquire atomic per key. A fencing token is monotonic per key: pass it to the
// protected resource so a stale holder (paused past its TTL) is rejected.
type Store interface {
	// Acquire claims key for owner until now+ttl, returning a monotonic fencing
	// token on success. ok is false if another live owner holds key. The fence
	// is guaranteed monotonically non-decreasing per key; it may or may not
	// advance on a re-acquire by the current owner.
	Acquire(ctx context.Context, key, owner string, ttl time.Duration) (fence uint64, ok bool, err error)
	// Refresh extends the lease iff owner still holds key; ok is false if lost.
	Refresh(ctx context.Context, key, owner string, ttl time.Duration) (ok bool, err error)
	// Release frees key iff held by owner (no-op otherwise).
	Release(ctx context.Context, key, owner string) error
}
