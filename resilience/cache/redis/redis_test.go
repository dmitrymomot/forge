//go:build integration

package redis_test

import (
	"context"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/cache"
	cacheredis "github.com/dmitrymomot/forge/resilience/cache/redis"
	"github.com/dmitrymomot/forge/testkit/redistest"
)

func testClient(t *testing.T) goredis.UniversalClient {
	t.Helper()
	c := goredis.NewClient(&goredis.Options{Addr: redistest.Addr(t)})
	require.NoError(t, c.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRedisStoreRoundTripAndScopedClear(t *testing.T) {
	client := testClient(t)
	store := cacheredis.NewStore(client)

	users := cache.New[string](store, cache.WithPrefix("test:cache:users:"))
	other := cache.New[string](store, cache.WithPrefix("test:cache:other:"))

	require.NoError(t, users.Set(t.Context(), "1", "alice", cache.WithTTL(time.Minute)))
	require.NoError(t, other.Set(t.Context(), "1", "keep", cache.WithTTL(time.Minute)))

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
	client := testClient(t)
	store := cacheredis.NewStore(client)

	// The prefix contains a glob metacharacter '['. DeletePrefix must match it
	// literally (like the in-memory store's strings.HasPrefix), not as a Redis
	// pattern — otherwise "g[x]" would also match the sibling "gx".
	target := cache.New[string](store, cache.WithPrefix("test:cache:g[x]:"))
	sibling := cache.New[string](store, cache.WithPrefix("test:cache:gx:"))

	require.NoError(t, target.Set(t.Context(), "1", "gone", cache.WithTTL(time.Minute)))
	require.NoError(t, sibling.Set(t.Context(), "1", "keep", cache.WithTTL(time.Minute)))

	require.NoError(t, target.Clear(t.Context()))

	_, err := target.Get(t.Context(), "1")
	assert.ErrorIs(t, err, cache.ErrNotFound)

	kept, err := sibling.Get(t.Context(), "1") // literal match: sibling untouched
	require.NoError(t, err)
	assert.Equal(t, "keep", kept)
}

func TestRedisStoreSetNonExistClaimsOnce(t *testing.T) {
	store := cacheredis.NewStore(testClient(t))

	key := "test:cache:nx:claim"
	t.Cleanup(func() { _ = store.Delete(context.Background(), key) })

	// First NX claim wins and takes a lease (SET key val NX PX ttl).
	require.NoError(t, store.Set(t.Context(), key, []byte("first"), cache.WithSetNonExist(), cache.WithTTL(time.Minute)))

	// A second NX claim on the live key is rejected: the server returns a null
	// reply (redis.Nil), which the backend maps to cache.ErrExists.
	err := store.Set(t.Context(), key, []byte("second"), cache.WithSetNonExist(), cache.WithTTL(time.Minute))
	assert.ErrorIs(t, err, cache.ErrExists)

	got, err := store.Get(t.Context(), key)
	require.NoError(t, err)
	assert.Equal(t, []byte("first"), got) // NX did not overwrite the original
}

func TestRedisStoreSetNonExistWithoutTTLPersists(t *testing.T) {
	client := testClient(t)
	store := cacheredis.NewStore(client)

	key := "test:cache:nx:nottl"
	t.Cleanup(func() { _ = store.Delete(context.Background(), key) })

	// NX claim with no WithTTL: SET key val NX with no PX. The claim still wins,
	// a live-key reclaim is still rejected, and — since no PX is sent — the key
	// has no expiry (redis reports TTL -1 for a persistent key).
	require.NoError(t, store.Set(t.Context(), key, []byte("v"), cache.WithSetNonExist()))
	assert.ErrorIs(t, store.Set(t.Context(), key, []byte("w"), cache.WithSetNonExist()), cache.ErrExists)
	assert.Equal(t, time.Duration(-1), client.TTL(t.Context(), key).Val())
}
