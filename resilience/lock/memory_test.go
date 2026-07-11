package lock_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/lock"
)

func TestMemoryStore_AcquireExclusiveAndFenceMonotonic(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	s := lock.NewMemoryStore(lock.WithMemoryClock(clk))
	ctx := context.Background()

	f1, ok, err := s.Acquire(ctx, "k", "owner-a", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, uint64(1), f1)

	_, ok, err = s.Acquire(ctx, "k", "owner-b", time.Minute) // held by a
	require.NoError(t, err)
	assert.False(t, ok)

	clk.Advance(2 * time.Minute) // a's lease expires
	f2, ok, err := s.Acquire(ctx, "k", "owner-b", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Greater(t, f2, f1) // fence strictly increases
}

func TestMemoryStore_RefreshAndRelease(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	s := lock.NewMemoryStore(lock.WithMemoryClock(clk))
	ctx := context.Background()
	_, ok, _ := s.Acquire(ctx, "k", "a", time.Minute)
	require.True(t, ok)

	ok, err := s.Refresh(ctx, "k", "a", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = s.Refresh(ctx, "k", "b", time.Minute) // not owner
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, s.Release(ctx, "k", "a"))
	_, ok, _ = s.Acquire(ctx, "k", "b", time.Minute) // now free
	assert.True(t, ok)
}
