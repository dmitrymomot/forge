package redisstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/dmitrymomot/forge/resilience/ratelimit/redisstore"
)

var _ ratelimit.Store = (*redisstore.Store)(nil)

func dial(t *testing.T) redis.UniversalClient {
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		t.Skip("set FORGE_TEST_REDIS_URL (host:port) to run the redis integration test")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
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
