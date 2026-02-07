//go:build integration

package cache_test

import (
	"context"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/cache"
	"github.com/dmitrymomot/forge/pkg/redis"
)

const testRedisURL = "redis://localhost:6379/0"

func newTestRedisClient(t *testing.T) goredis.UniversalClient {
	t.Helper()

	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = testRedisURL
	}

	ctx := context.Background()
	client, err := redis.Open(ctx, url)
	require.NoError(t, err, "failed to connect to Redis")

	t.Cleanup(func() {
		_ = client.FlushDB(ctx).Err()
		_ = client.Close()
	})

	return client
}

// --- Redis: Get ---

func TestRedis_Get(t *testing.T) {
	t.Parallel()

	t.Run("returns ErrNotFound for missing key", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil, cache.WithPrefix("test-get-miss"))

		_, err := c.Get(context.Background(), "missing")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("returns stored value", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[int](client, nil, cache.WithPrefix("test-get-hit"))

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", 42, time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, 42, val)
	})

	t.Run("returns ErrNotFound for expired key", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil, cache.WithPrefix("test-get-expired"))

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", 50*time.Millisecond))

		time.Sleep(100 * time.Millisecond)

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})
}

// --- Redis: Set ---

func TestRedis_Set(t *testing.T) {
	t.Parallel()

	t.Run("stores and retrieves value", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil, cache.WithPrefix("test-set"))

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)
	})

	t.Run("zero TTL uses default", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil,
			cache.WithPrefix("test-set-default-ttl"),
			cache.WithRedisDefaultTTL(100*time.Millisecond),
		)

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", 0))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)

		time.Sleep(200 * time.Millisecond)

		_, err = c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("negative TTL persists indefinitely", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil,
			cache.WithPrefix("test-set-no-expire"),
			cache.WithRedisDefaultTTL(50*time.Millisecond),
		)

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "forever", -1))

		time.Sleep(100 * time.Millisecond)

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "forever", val)
	})

	t.Run("overwrites existing key", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[int](client, nil, cache.WithPrefix("test-set-overwrite"))

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", 1, time.Minute))
		require.NoError(t, c.Set(ctx, "key", 2, time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, 2, val)
	})

	t.Run("stores struct values", func(t *testing.T) {
		t.Parallel()

		type user struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}

		client := newTestRedisClient(t)
		c := cache.NewRedis[user](client, nil, cache.WithPrefix("test-set-struct"))

		ctx := context.Background()
		u := user{Name: "Alice", Age: 30}
		require.NoError(t, c.Set(ctx, "user", u, time.Minute))

		val, err := c.Get(ctx, "user")
		require.NoError(t, err)
		require.Equal(t, u, val)
	})
}

// --- Redis: Delete ---

func TestRedis_Delete(t *testing.T) {
	t.Parallel()

	t.Run("removes existing key", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil, cache.WithPrefix("test-del"))

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))
		require.NoError(t, c.Delete(ctx, "key"))

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("no error for missing key", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil, cache.WithPrefix("test-del-miss"))

		err := c.Delete(context.Background(), "missing")
		require.NoError(t, err)
	})
}

// --- Redis: Has ---

func TestRedis_Has(t *testing.T) {
	t.Parallel()

	t.Run("returns true for existing key", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil, cache.WithPrefix("test-has"))

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		has, err := c.Has(ctx, "key")
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("returns false for missing key", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil, cache.WithPrefix("test-has-miss"))

		has, err := c.Has(context.Background(), "missing")
		require.NoError(t, err)
		require.False(t, has)
	})
}

// --- Redis: Clear ---

func TestRedis_Clear(t *testing.T) {
	t.Parallel()

	t.Run("clears only prefixed keys with prefix", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c1 := cache.NewRedis[string](client, nil, cache.WithPrefix("test-clear-ns1"))
		c2 := cache.NewRedis[string](client, nil, cache.WithPrefix("test-clear-ns2"))

		ctx := context.Background()
		require.NoError(t, c1.Set(ctx, "a", "1", time.Minute))
		require.NoError(t, c1.Set(ctx, "b", "2", time.Minute))
		require.NoError(t, c2.Set(ctx, "c", "3", time.Minute))

		// Clear ns1 only.
		require.NoError(t, c1.Clear(ctx))

		has, err := c1.Has(ctx, "a")
		require.NoError(t, err)
		require.False(t, has, "ns1:a should be cleared")

		has, err = c1.Has(ctx, "b")
		require.NoError(t, err)
		require.False(t, has, "ns1:b should be cleared")

		// ns2 should be unaffected.
		has, err = c2.Has(ctx, "c")
		require.NoError(t, err)
		require.True(t, has, "ns2:c should still exist")
	})
}

// --- Redis: Prefix ---

func TestRedis_Prefix(t *testing.T) {
	t.Parallel()

	t.Run("different prefixes are isolated", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c1 := cache.NewRedis[string](client, nil, cache.WithPrefix("test-prefix-iso1"))
		c2 := cache.NewRedis[string](client, nil, cache.WithPrefix("test-prefix-iso2"))

		ctx := context.Background()
		require.NoError(t, c1.Set(ctx, "key", "from-c1", time.Minute))
		require.NoError(t, c2.Set(ctx, "key", "from-c2", time.Minute))

		v1, err := c1.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "from-c1", v1)

		v2, err := c2.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "from-c2", v2)
	})
}

// --- Redis: Close ---

func TestRedis_Close(t *testing.T) {
	t.Parallel()

	t.Run("close is no-op", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, nil)

		require.NoError(t, c.Close())
		require.NoError(t, c.Close())
	})
}

// --- Redis: Custom Marshaler ---

type reversedMarshaler struct{}

func (reversedMarshaler) Marshal(v string) ([]byte, error) {
	runes := []rune(v)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return []byte(string(runes)), nil
}

func (reversedMarshaler) Unmarshal(data []byte) (string, error) {
	runes := []rune(string(data))
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes), nil
}

func TestRedis_CustomMarshaler(t *testing.T) {
	t.Parallel()

	t.Run("uses custom marshaler for serialization", func(t *testing.T) {
		t.Parallel()

		client := newTestRedisClient(t)
		c := cache.NewRedis[string](client, reversedMarshaler{}, cache.WithPrefix("test-custom-marshal"))

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "hello", time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "hello", val) // Round-trip should restore original.

		// Verify the raw value in Redis is reversed.
		raw, err := client.Get(ctx, "test-custom-marshal:key").Result()
		require.NoError(t, err)
		require.Equal(t, "olleh", raw)
	})
}
