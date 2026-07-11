package lock_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/lock"
)

func TestRunOnLeader_OnlyOneRunsAtATime(t *testing.T) {
	// Two distinct-owner Locks over the same store make this a real election:
	// the leader holds the key, and the follower's different-owner Acquire
	// blocks until the leader releases or its lease expires.
	store := lock.NewMemoryStore()
	lA := lock.New(store, lock.WithOwner("node-a"), lock.WithTTL(40*time.Millisecond), lock.WithRefreshInterval(10*time.Millisecond))
	lB := lock.New(store, lock.WithOwner("node-b"), lock.WithTTL(40*time.Millisecond), lock.WithRefreshInterval(10*time.Millisecond))

	var running atomic.Int32
	var maxSeen atomic.Int32
	work := func(ctx context.Context) error {
		n := running.Add(1)
		for {
			if m := maxSeen.Load(); n > m {
				maxSeen.CompareAndSwap(m, n)
			}
			select {
			case <-ctx.Done():
				running.Add(-1)
				return ctx.Err()
			case <-time.After(2 * time.Millisecond):
			}
		}
	}

	svcA := lA.RunOnLeader("worker", "leader-key", work)
	svcB := lB.RunOnLeader("worker", "leader-key", work)
	assert.Equal(t, "worker", svcA.Name())

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = svcA.Run(ctx) }()
	go func() { _ = svcB.Run(ctx) }()

	time.Sleep(150 * time.Millisecond)
	assert.LessOrEqual(t, maxSeen.Load(), int32(1), "at most one leader runs at a time")
	cancel()
	time.Sleep(60 * time.Millisecond)
}
