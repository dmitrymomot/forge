package lock

import (
	"context"
	"sync"
	"time"
)

// Lease is a held distributed lock. A background goroutine refreshes it until
// Release or ctx-cancel; if a refresh fails (expired or stolen) Done closes and
// the holder must stop its critical section.
type Lease struct {
	lock   *Lock
	cancel context.CancelFunc
	done   chan struct{}
	key    string
	fence  uint64
	once   sync.Once
}

func (l *Lock) newLease(key string, fence uint64) *Lease {
	ctx, cancel := context.WithCancel(context.Background())
	le := &Lease{lock: l, key: key, fence: fence, cancel: cancel, done: make(chan struct{})}
	go le.refreshLoop(ctx)
	return le
}

// Fence returns the monotonic fencing token for this lease.
func (le *Lease) Fence() uint64 { return le.fence }

// Done is closed when the lease is lost (a refresh failed) or after Release.
func (le *Lease) Done() <-chan struct{} { return le.done }

// Release frees the lease and stops refreshing. Safe to call multiple times.
func (le *Lease) Release(ctx context.Context) error {
	le.stop()
	return le.lock.store.Release(ctx, le.key, le.lock.cfg.owner)
}

func (le *Lease) stop() {
	le.once.Do(func() {
		le.cancel()
		close(le.done)
	})
}

func (le *Lease) refreshLoop(ctx context.Context) {
	t := time.NewTicker(le.lock.cfg.refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ok, err := le.lock.store.Refresh(ctx, le.key, le.lock.cfg.owner, le.lock.cfg.ttl)
			if err != nil || !ok {
				le.stop() // lease lost
				return
			}
		}
	}
}
