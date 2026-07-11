package lock

import (
	"context"
	"fmt"
	"time"
)

// Lock issues distributed leases over a Store. All leases from one Lock share
// its owner id.
type Lock struct {
	store Store
	cfg   config
}

// New builds a Lock. RefreshInterval defaults to TTL/3 when unset.
func New(store Store, opts ...Option) *Lock {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	if c.refresh <= 0 {
		c.refresh = c.ttl / 3
	}
	if c.refresh <= 0 {
		c.refresh = c.ttl
	}
	return &Lock{store: store, cfg: c}
}

// TryAcquire makes a single attempt to claim key. ok is false if already held.
func (l *Lock) TryAcquire(ctx context.Context, key string) (*Lease, bool, error) {
	fence, ok, err := l.store.Acquire(ctx, key, l.cfg.owner, l.cfg.ttl)
	if err != nil || !ok {
		return nil, ok, err
	}
	return l.newLease(key, fence), true, nil
}

// Acquire blocks, retrying at the refresh cadence, until key is held or ctx is
// cancelled.
func (l *Lock) Acquire(ctx context.Context, key string) (*Lease, error) {
	for {
		lease, ok, err := l.TryAcquire(ctx, key)
		if err != nil {
			return nil, err
		}
		if ok {
			return lease, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("lock: acquire %q: %w", key, ctx.Err())
		case <-time.After(l.cfg.refresh):
		}
	}
}
