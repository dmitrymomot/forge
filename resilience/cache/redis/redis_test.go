package redis_test

import (
	"context"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/cache"
	cacheredis "github.com/dmitrymomot/forge/resilience/cache/redis"
)

func testClient(t *testing.T) goredis.UniversalClient {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opt, err := goredis.ParseURL(url)
	require.NoError(t, err)
	c := goredis.NewClient(opt)
	require.NoError(t, c.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRedisStoreRoundTripAndScopedClear(t *testing.T) {
	client := testClient(t)
	store := cacheredis.NewStore(client)

	users := cache.New[string](store, cache.WithPrefix("test:cache:users:"))
	other := cache.New[string](store, cache.WithPrefix("test:cache:other:"))

	require.NoError(t, users.Set(t.Context(), "1", "alice", time.Minute))
	require.NoError(t, other.Set(t.Context(), "1", "keep", time.Minute))

	v, err := users.Get(t.Context(), "1")
	require.NoError(t, err)
	assert.Equal(t, "alice", v)

	require.NoError(t, users.Clear(t.Context()))
	_, err = users.Get(t.Context(), "1")
	assert.ErrorIs(t, err, cache.ErrNotFound)

	kept, err := other.Get(t.Context(), "1") // scoped clear left other's prefix alone
	require.NoError(t, err)
	assert.Equal(t, "keep", kept)
}

func TestRedisStoreDeletePrefixMatchesLiterally(t *testing.T) {
	client := testClient(t) // SKIPs without TEST_REDIS_URL
	store := cacheredis.NewStore(client)

	// The prefix contains a glob metacharacter '['. DeletePrefix must match it
	// literally (like the in-memory store's strings.HasPrefix), not as a Redis
	// pattern — otherwise "g[x]" would also match the sibling "gx".
	target := cache.New[string](store, cache.WithPrefix("test:cache:g[x]:"))
	sibling := cache.New[string](store, cache.WithPrefix("test:cache:gx:"))

	require.NoError(t, target.Set(t.Context(), "1", "gone", time.Minute))
	require.NoError(t, sibling.Set(t.Context(), "1", "keep", time.Minute))

	require.NoError(t, target.Clear(t.Context()))

	_, err := target.Get(t.Context(), "1")
	assert.ErrorIs(t, err, cache.ErrNotFound)

	kept, err := sibling.Get(t.Context(), "1") // literal match: sibling untouched
	require.NoError(t, err)
	assert.Equal(t, "keep", kept)
}
