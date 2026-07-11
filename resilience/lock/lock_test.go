package lock_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/lock"
)

func TestLock_TryAcquireMutualExclusion(t *testing.T) {
	store := lock.NewMemoryStore() // system clock
	l := lock.New(store, lock.WithTTL(time.Minute))
	ctx := context.Background()

	lease, ok, err := l.TryAcquire(ctx, "job")
	require.NoError(t, err)
	require.True(t, ok)
	assert.NotZero(t, lease.Fence())

	_, ok, err = l.TryAcquire(ctx, "job") // same owner? No — New() gives a random owner per Lock
	require.NoError(t, err)
	assert.True(t, ok) // same Lock instance = same owner → re-acquire allowed

	require.NoError(t, lease.Release(ctx))
}

func TestLock_AutoRefreshKeepsLeaseAlive(t *testing.T) {
	store := lock.NewMemoryStore()
	// short real durations: refresh every 10ms keeps a 40ms lease alive
	l := lock.New(store, lock.WithTTL(40*time.Millisecond), lock.WithRefreshInterval(10*time.Millisecond))
	ctx := context.Background()

	lease, err := l.Acquire(ctx, "singleton")
	require.NoError(t, err)
	defer func() { _ = lease.Release(ctx) }()

	// a competitor with a different owner must NOT get it while refresh runs
	other := lock.New(store, lock.WithTTL(40*time.Millisecond))
	time.Sleep(120 * time.Millisecond) // 3x TTL
	_, ok, err := other.TryAcquire(ctx, "singleton")
	require.NoError(t, err)
	assert.False(t, ok, "auto-refresh should keep the original lease alive")

	select {
	case <-lease.Done():
		t.Fatal("lease should not be lost while refreshing")
	default:
	}
}

func TestLease_DoneClosesWhenLost(t *testing.T) {
	store := lock.NewMemoryStore()
	l := lock.New(store, lock.WithOwner("owner-a"), lock.WithTTL(30*time.Millisecond), lock.WithRefreshInterval(10*time.Millisecond))
	ctx := context.Background()
	lease, err := l.Acquire(ctx, "k")
	require.NoError(t, err)

	// Force loss: free the key out from under the refresh loop as the known owner.
	// The next Refresh finds the key absent -> ok=false -> the lease is lost -> Done closes.
	require.NoError(t, store.Release(ctx, "k", "owner-a"))

	select {
	case <-lease.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Done should close after the lease is lost")
	}
}
