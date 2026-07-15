//go:build integration

package redisstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/dmitrymomot/forge/resilience/ratelimit/redisstore"
	"github.com/dmitrymomot/forge/testkit/redistest"
)

var _ ratelimit.Store = (*redisstore.Store)(nil)

func dial(t *testing.T) redis.UniversalClient {
	c := redis.NewClient(&redis.Options{Addr: redistest.Addr(t)})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRedisStore_IncrTTLAtomic(t *testing.T) {
	client := dial(t)
	s := redisstore.New(client, redisstore.WithPrefix("rltest:"))
	ctx := context.Background()
	key := "k-" + t.Name()
	require.NoError(t, s.Reset(ctx, key))

	n, err := s.Incr(ctx, key, 1, 500*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	n, err = s.Incr(ctx, key, 1, 500*time.Millisecond) // must NOT re-arm TTL
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	time.Sleep(600 * time.Millisecond)
	got, err := s.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, int64(0), got) // expired at the FIRST incr's TTL, not extended
}

func TestRedisStore_NonPositiveTTLNeverExpires(t *testing.T) {
	client := dial(t)
	s := redisstore.New(client, redisstore.WithPrefix("rltest:"))
	ctx := context.Background()
	key := "gauge-" + t.Name()
	require.NoError(t, s.Reset(ctx, key))

	n, err := s.Incr(ctx, key, 5, 0) // no expiry
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)

	ttl, err := client.PTTL(ctx, "rltest:"+key).Result()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), ttl) // -1ms = key exists with no TTL

	require.NoError(t, s.Reset(ctx, key))
}
