//go:build integration

package redisstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/lock"
	"github.com/dmitrymomot/forge/resilience/lock/redisstore"
	"github.com/dmitrymomot/forge/testkit/redistest"
)

var _ lock.Store = (*redisstore.Store)(nil)

func dial(t *testing.T) redis.UniversalClient {
	c := redis.NewClient(&redis.Options{Addr: redistest.Addr(t)})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRedisLock_AcquireExclusiveFenceRefreshRelease(t *testing.T) {
	s := redisstore.New(dial(t), redisstore.WithPrefix("locktest:"))
	ctx := context.Background()
	key := "k-" + t.Name()
	_ = s.Release(ctx, key, "a")
	_ = s.Release(ctx, key, "b")

	f1, ok, err := s.Acquire(ctx, key, "a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.NotZero(t, f1)

	_, ok, err = s.Acquire(ctx, key, "b", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = s.Refresh(ctx, key, "a", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = s.Refresh(ctx, key, "b", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, s.Release(ctx, key, "a"))
	f2, ok, err := s.Acquire(ctx, key, "b", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Greater(t, f2, f1)
	require.NoError(t, s.Release(ctx, key, "b"))
}
