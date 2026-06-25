package cache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/cache"
)

// --- Memory: Get ---

func TestMemory_Get(t *testing.T) {
	t.Parallel()

	t.Run("returns ErrNotFound for missing key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		_, err := c.Get(context.Background(), "missing")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("returns stored value", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", 42, time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, 42, val)
	})

	t.Run("returns ErrNotFound for expired key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{CleanupInterval: -1})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Millisecond))

		time.Sleep(5 * time.Millisecond)

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("marks entry as recently used", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{MaxEntries: 2})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "a", "1", time.Minute))
		require.NoError(t, c.Set(ctx, "b", "2", time.Minute))

		// Access "a" to make it recently used.
		_, err := c.Get(ctx, "a")
		require.NoError(t, err)

		// Add "c" — should evict "b" (LRU), not "a".
		require.NoError(t, c.Set(ctx, "c", "3", time.Minute))

		has, err := c.Has(ctx, "a")
		require.NoError(t, err)
		require.True(t, has, "a should still exist (recently used)")

		has, err = c.Has(ctx, "b")
		require.NoError(t, err)
		require.False(t, has, "b should have been evicted")
	})
}

// --- Memory: Set ---

func TestMemory_Set(t *testing.T) {
	t.Parallel()

	t.Run("stores and retrieves value", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)
	})

	t.Run("zero TTL uses default", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{DefaultTTL: 50 * time.Millisecond, CleanupInterval: -1})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", 0))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)

		time.Sleep(60 * time.Millisecond)

		_, err = c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("negative TTL never expires", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{DefaultTTL: 10 * time.Millisecond, CleanupInterval: -1})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "forever", -1))

		time.Sleep(20 * time.Millisecond)

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "forever", val)
	})

	t.Run("overwrites existing key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", 1, time.Minute))
		require.NoError(t, c.Set(ctx, "key", 2, time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, 2, val)
	})

	t.Run("returns ErrClosed after Close", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		require.NoError(t, c.Close())

		err := c.Set(context.Background(), "key", "value", time.Minute)
		require.ErrorIs(t, err, cache.ErrClosed)
	})
}

// --- Memory: Delete ---

func TestMemory_Delete(t *testing.T) {
	t.Parallel()

	t.Run("removes existing key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))
		require.NoError(t, c.Delete(ctx, "key"))

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("no error for missing key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		err := c.Delete(context.Background(), "missing")
		require.NoError(t, err)
	})

	t.Run("returns ErrClosed after Close", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		require.NoError(t, c.Close())

		err := c.Delete(context.Background(), "key")
		require.ErrorIs(t, err, cache.ErrClosed)
	})
}

// --- Memory: Has ---

func TestMemory_Has(t *testing.T) {
	t.Parallel()

	t.Run("returns true for existing key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		has, err := c.Has(ctx, "key")
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("returns false for missing key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		has, err := c.Has(context.Background(), "missing")
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("returns false for expired key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{CleanupInterval: -1})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Millisecond))

		time.Sleep(5 * time.Millisecond)

		has, err := c.Has(ctx, "key")
		require.NoError(t, err)
		require.False(t, has)
	})
}

// --- Memory: Clear ---

func TestMemory_Clear(t *testing.T) {
	t.Parallel()

	t.Run("removes all entries", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "a", "1", time.Minute))
		require.NoError(t, c.Set(ctx, "b", "2", time.Minute))
		require.NoError(t, c.Set(ctx, "c", "3", time.Minute))

		require.NoError(t, c.Clear(ctx))

		has, _ := c.Has(ctx, "a")
		require.False(t, has)
		has, _ = c.Has(ctx, "b")
		require.False(t, has)
		has, _ = c.Has(ctx, "c")
		require.False(t, has)
	})

	t.Run("returns ErrClosed after Close", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		require.NoError(t, c.Close())

		err := c.Clear(context.Background())
		require.ErrorIs(t, err, cache.ErrClosed)
	})
}

// --- Memory: Close ---

func TestMemory_Close(t *testing.T) {
	t.Parallel()

	t.Run("idempotent close", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		require.NoError(t, c.Close())
		require.NoError(t, c.Close())
	})
}

// --- Memory: read-after-Close returns ErrClosed ---

func TestMemory_ReadAfterClose(t *testing.T) {
	t.Parallel()

	t.Run("Get returns ErrClosed after Close", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))
		require.NoError(t, c.Close())

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrClosed)
	})

	t.Run("Has returns ErrClosed after Close", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))
		require.NoError(t, c.Close())

		_, err := c.Has(ctx, "key")
		require.ErrorIs(t, err, cache.ErrClosed)
	})
}

// --- Memory: MaxEntries / LRU ---

func TestMemory_MaxEntries(t *testing.T) {
	t.Parallel()

	t.Run("evicts LRU when at capacity", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{MaxEntries: 3})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "a", 1, time.Minute))
		require.NoError(t, c.Set(ctx, "b", 2, time.Minute))
		require.NoError(t, c.Set(ctx, "c", 3, time.Minute))

		// Add one more — should evict "a" (least recently used).
		require.NoError(t, c.Set(ctx, "d", 4, time.Minute))

		_, err := c.Get(ctx, "a")
		require.ErrorIs(t, err, cache.ErrNotFound, "a should have been evicted")

		val, err := c.Get(ctx, "d")
		require.NoError(t, err)
		require.Equal(t, 4, val)
	})

	t.Run("no eviction when under capacity", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{MaxEntries: 5})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "a", 1, time.Minute))
		require.NoError(t, c.Set(ctx, "b", 2, time.Minute))

		has, err := c.Has(ctx, "a")
		require.NoError(t, err)
		require.True(t, has)

		has, err = c.Has(ctx, "b")
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("overwrite does not count as new entry", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{MaxEntries: 2})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "a", 1, time.Minute))
		require.NoError(t, c.Set(ctx, "b", 2, time.Minute))

		// Overwrite "a" — should NOT evict "b".
		require.NoError(t, c.Set(ctx, "a", 10, time.Minute))

		val, err := c.Get(ctx, "a")
		require.NoError(t, err)
		require.Equal(t, 10, val)

		val, err = c.Get(ctx, "b")
		require.NoError(t, err)
		require.Equal(t, 2, val)
	})

	t.Run("put updates recency for LRU", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{MaxEntries: 3})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "a", 1, time.Minute))
		require.NoError(t, c.Set(ctx, "b", 2, time.Minute))
		require.NoError(t, c.Set(ctx, "c", 3, time.Minute))

		// Update "a" to make it recently used.
		require.NoError(t, c.Set(ctx, "a", 10, time.Minute))

		// Add "d" — should evict "b" (now LRU).
		require.NoError(t, c.Set(ctx, "d", 4, time.Minute))

		_, err := c.Get(ctx, "b")
		require.ErrorIs(t, err, cache.ErrNotFound, "b should have been evicted")

		val, err := c.Get(ctx, "a")
		require.NoError(t, err)
		require.Equal(t, 10, val)
	})

	t.Run("capacity of 1", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{MaxEntries: 1})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "a", 1, time.Minute))
		require.NoError(t, c.Set(ctx, "b", 2, time.Minute))

		_, err := c.Get(ctx, "a")
		require.ErrorIs(t, err, cache.ErrNotFound)

		val, err := c.Get(ctx, "b")
		require.NoError(t, err)
		require.Equal(t, 2, val)
	})
}

// --- Memory: Eviction Callback ---

func TestMemory_EvictCallback(t *testing.T) {
	t.Parallel()

	t.Run("called on LRU eviction", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{MaxEntries: 2})
		defer c.Close()

		var mu sync.Mutex
		evicted := make(map[string]int)
		c.SetEvictCallback(func(key string, value int) {
			mu.Lock()
			evicted[key] = value
			mu.Unlock()
		})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "a", 1, time.Minute))
		require.NoError(t, c.Set(ctx, "b", 2, time.Minute))
		require.NoError(t, c.Set(ctx, "c", 3, time.Minute))

		mu.Lock()
		require.Equal(t, 1, evicted["a"], "a should have been evicted with value 1")
		mu.Unlock()
	})

	t.Run("called on Delete", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		var evictedKey string
		c.SetEvictCallback(func(key string, _ string) {
			evictedKey = key
		})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))
		require.NoError(t, c.Delete(ctx, "key"))

		require.Equal(t, "key", evictedKey)
	})

	t.Run("called on Clear", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{})
		defer c.Close()

		var mu sync.Mutex
		evicted := make(map[string]int)
		c.SetEvictCallback(func(key string, value int) {
			mu.Lock()
			evicted[key] = value
			mu.Unlock()
		})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "a", 1, time.Minute))
		require.NoError(t, c.Set(ctx, "b", 2, time.Minute))
		require.NoError(t, c.Clear(ctx))

		mu.Lock()
		require.Equal(t, 1, evicted["a"])
		require.Equal(t, 2, evicted["b"])
		mu.Unlock()
	})
}

// --- Memory: Janitor ---

func TestMemory_Janitor(t *testing.T) {
	t.Parallel()

	t.Run("removes expired entries periodically", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{
			CleanupInterval: 10 * time.Millisecond,
		})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "short", "value", 20*time.Millisecond))
		require.NoError(t, c.Set(ctx, "long", "value", time.Minute))

		// Wait for TTL + cleanup cycle.
		time.Sleep(50 * time.Millisecond)

		has, _ := c.Has(ctx, "short")
		require.False(t, has, "short should have been cleaned up by janitor")

		has, _ = c.Has(ctx, "long")
		require.True(t, has, "long should still exist")
	})
}

// --- Memory: Concurrent Access ---

func TestMemory_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	t.Run("concurrent reads and writes", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{MaxEntries: 100})
		defer c.Close()

		ctx := context.Background()
		var wg sync.WaitGroup

		// Concurrent writers.
		for i := range 50 {
			wg.Go(func() {
				_ = c.Set(ctx, "key", i, time.Minute)
			})
		}

		// Concurrent readers.
		for range 50 {
			wg.Go(func() {
				_, _ = c.Get(ctx, "key")
			})
		}

		// Concurrent deleters.
		for range 10 {
			wg.Go(func() {
				_ = c.Delete(ctx, "key")
			})
		}

		wg.Wait()
	})
}

// --- GetOrSet ---

func TestGetOrSet(t *testing.T) {
	t.Parallel()

	t.Run("returns cached value on hit", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "cached", time.Minute))

		val, err := cache.GetOrSet(ctx, c, "key", func(_ context.Context) (string, time.Duration, error) {
			t.Fatal("fn should not be called on cache hit")
			return "", 0, nil
		})
		require.NoError(t, err)
		require.Equal(t, "cached", val)
	})

	t.Run("calls fn on miss and caches result", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		val, err := cache.GetOrSet(ctx, c, "key", func(_ context.Context) (string, time.Duration, error) {
			return "computed", time.Minute, nil
		})
		require.NoError(t, err)
		require.Equal(t, "computed", val)

		// Verify it was cached.
		cached, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "computed", cached)
	})

	t.Run("returns error from fn", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		testErr := errors.New("compute failed")

		_, err := cache.GetOrSet(ctx, c, "key", func(_ context.Context) (string, time.Duration, error) {
			return "", 0, testErr
		})
		require.ErrorIs(t, err, testErr)

		// Verify nothing was cached.
		_, err = c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("deduplicates concurrent calls", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		var calls atomic.Int64
		var wg sync.WaitGroup

		for range 10 {
			wg.Go(func() {
				val, err := cache.GetOrSet(ctx, c, "dedup", func(_ context.Context) (int, time.Duration, error) {
					calls.Add(1)
					time.Sleep(10 * time.Millisecond) // Simulate slow computation.
					return 42, time.Minute, nil
				})
				require.NoError(t, err)
				require.Equal(t, 42, val)
			})
		}

		wg.Wait()

		// singleflight should have deduplicated: fn called at most a few times
		// (once for the initial miss, possibly once more if the first call completes
		// before others arrive at the singleflight).
		require.LessOrEqual(t, calls.Load(), int64(2),
			"fn should be called at most twice due to singleflight dedup")
	})

	t.Run("Set runs once per computation, not once per waiting caller", func(t *testing.T) {
		t.Parallel()

		// countingCache wraps a real Memory cache and counts Set calls. It
		// embeds *Memory[int] so the promoted sfDo method makes it satisfy the
		// unexported deduper interface, routing GetOrSet through singleflight.
		base := cache.NewMemory[int](cache.MemoryConfig{})
		defer base.Close()
		c := &countingCache[int]{Memory: base}

		ctx := context.Background()
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				val, err := cache.GetOrSet(ctx, c, "key", func(_ context.Context) (int, time.Duration, error) {
					time.Sleep(10 * time.Millisecond) // Hold the singleflight open.
					return 7, time.Minute, nil
				})
				require.NoError(t, err)
				require.Equal(t, 7, val)
			})
		}
		wg.Wait()

		// With Set inside the singleflight callback, each deduplicated
		// computation performs exactly one Set — not one per waiting caller.
		require.LessOrEqual(t, c.sets.Load(), int64(2),
			"Set should run at most once per deduplicated computation, not per caller")
	})
}

// countingCache wraps a Memory cache and counts Set invocations. Embedding
// *cache.Memory promotes its (unexported) sfDo method so GetOrSet treats this
// wrapper as a deduper.
type countingCache[V any] struct {
	*cache.Memory[V]
	sets atomic.Int64
}

func (c *countingCache[V]) Set(ctx context.Context, key string, value V, ttl time.Duration) error {
	c.sets.Add(1)
	return c.Memory.Set(ctx, key, value, ttl)
}

// --- JSON Marshaler ---

func TestJsonMarshaler(t *testing.T) {
	t.Parallel()

	t.Run("marshal and unmarshal struct", func(t *testing.T) {
		t.Parallel()

		type user struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}

		// Use Redis cache constructor to exercise the default JSON marshaler indirectly.
		// Instead, test via round-trip through memory cache with a struct value.
		c := cache.NewMemory[user](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		u := user{Name: "Alice", Age: 30}
		require.NoError(t, c.Set(ctx, "user", u, time.Minute))

		val, err := c.Get(ctx, "user")
		require.NoError(t, err)
		require.Equal(t, u, val)
	})
}

// --- Memory Options ---

func TestMemoryOptions(t *testing.T) {
	t.Parallel()

	t.Run("DefaultTTL config sets default TTL", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{
			DefaultTTL:      20 * time.Millisecond,
			CleanupInterval: -1,
		})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", 0)) // Uses default.

		time.Sleep(30 * time.Millisecond)

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("default options are sensible", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.MemoryConfig{})
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", 0))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)
	})
}

// --- Redis test helpers ---

func newTestRedis(t *testing.T) (goredis.UniversalClient, *miniredis.Miniredis) {
	t.Helper()
	s := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client, s
}

// --- Redis: Get ---

func TestRedis_Get(t *testing.T) {
	t.Parallel()

	t.Run("returns ErrNotFound for missing key", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-get-miss"})

		_, err := c.Get(context.Background(), "missing")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("returns stored value", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[int](client, nil, cache.RedisConfig{Prefix: "test-get-hit"})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", 42, time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, 42, val)
	})

	t.Run("returns ErrNotFound for expired key", func(t *testing.T) {
		t.Parallel()

		client, s := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-get-expired"})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", 50*time.Millisecond))

		s.FastForward(100 * time.Millisecond)

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})
}

// --- Redis: Set ---

func TestRedis_Set(t *testing.T) {
	t.Parallel()

	t.Run("stores and retrieves value", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-set"})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)
	})

	t.Run("zero TTL uses default", func(t *testing.T) {
		t.Parallel()

		client, s := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{
			Prefix:     "test-set-default-ttl",
			DefaultTTL: 100 * time.Millisecond,
		})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", 0))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)

		s.FastForward(200 * time.Millisecond)

		_, err = c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("negative TTL persists indefinitely", func(t *testing.T) {
		t.Parallel()

		client, s := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{
			Prefix:     "test-set-no-expire",
			DefaultTTL: 50 * time.Millisecond,
		})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "forever", -1))

		s.FastForward(100 * time.Millisecond)

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "forever", val)
	})

	t.Run("overwrites existing key", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[int](client, nil, cache.RedisConfig{Prefix: "test-set-overwrite"})

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

		client, _ := newTestRedis(t)
		c := cache.NewRedis[user](client, nil, cache.RedisConfig{Prefix: "test-set-struct"})

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

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-del"})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))
		require.NoError(t, c.Delete(ctx, "key"))

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("no error for missing key", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-del-miss"})

		err := c.Delete(context.Background(), "missing")
		require.NoError(t, err)
	})
}

// --- Redis: Has ---

func TestRedis_Has(t *testing.T) {
	t.Parallel()

	t.Run("returns true for existing key", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-has"})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		has, err := c.Has(ctx, "key")
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("returns false for missing key", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-has-miss"})

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

		client, _ := newTestRedis(t)
		c1 := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-clear-ns1"})
		c2 := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-clear-ns2"})

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

		client, _ := newTestRedis(t)
		c1 := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-prefix-iso1"})
		c2 := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-prefix-iso2"})

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

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{})

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

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, reversedMarshaler{}, cache.RedisConfig{Prefix: "test-custom-marshal"})

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

// --- Redis: Clear empty-prefix guard ---

func TestRedis_Clear_EmptyPrefix(t *testing.T) {
	t.Parallel()

	t.Run("returns ErrNoPrefix instead of FLUSHDB", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{}) // no prefix

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		// Clear must refuse to wipe the whole DB.
		err := c.Clear(ctx)
		require.ErrorIs(t, err, cache.ErrNoPrefix)

		// The data must still be present — nothing was flushed.
		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)
	})

	t.Run("FlushDB explicitly wipes the database", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{}) // no prefix

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		require.NoError(t, c.FlushDB(ctx))

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})
}

// --- Redis: Marshal / Unmarshal errors ---

// unmarshalable holds a channel, which encoding/json cannot serialize.
type unmarshalable struct {
	Ch chan int `json:"ch"`
}

func TestRedis_MarshalError(t *testing.T) {
	t.Parallel()

	t.Run("Set wraps ErrMarshal for unmarshalable value", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[unmarshalable](client, nil, cache.RedisConfig{Prefix: "test-marshal-err"})

		ctx := context.Background()
		err := c.Set(ctx, "key", unmarshalable{Ch: make(chan int)}, time.Minute)
		require.ErrorIs(t, err, cache.ErrMarshal)

		// Nothing should have been written.
		_, err = client.Get(ctx, "test-marshal-err:key").Result()
		require.ErrorIs(t, err, goredis.Nil)
	})
}

func TestRedis_UnmarshalError(t *testing.T) {
	t.Parallel()

	t.Run("Get wraps ErrUnmarshal for corrupt bytes", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[int](client, nil, cache.RedisConfig{Prefix: "test-unmarshal-err"})

		ctx := context.Background()
		// Write bytes that are not valid JSON for an int directly to Redis.
		require.NoError(t, client.Set(ctx, "test-unmarshal-err:key", "not-a-number", time.Minute).Err())

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrUnmarshal)
	})
}

// --- Redis: GetOrSet / singleflight ---

func TestRedis_GetOrSet(t *testing.T) {
	t.Parallel()

	t.Run("calls fn on miss and caches result", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-redis-getorset"})

		ctx := context.Background()
		val, err := cache.GetOrSet(ctx, c, "key", func(_ context.Context) (string, time.Duration, error) {
			return "computed", time.Minute, nil
		})
		require.NoError(t, err)
		require.Equal(t, "computed", val)

		// Verify it was cached in Redis.
		cached, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "computed", cached)
	})

	t.Run("returns cached value on hit", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[string](client, nil, cache.RedisConfig{Prefix: "test-redis-getorset-hit"})

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "cached", time.Minute))

		val, err := cache.GetOrSet(ctx, c, "key", func(_ context.Context) (string, time.Duration, error) {
			t.Fatal("fn should not be called on cache hit")
			return "", 0, nil
		})
		require.NoError(t, err)
		require.Equal(t, "cached", val)
	})

	t.Run("deduplicates concurrent calls via singleflight", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		c := cache.NewRedis[int](client, nil, cache.RedisConfig{Prefix: "test-redis-getorset-dedup"})

		ctx := context.Background()
		var calls atomic.Int64
		var wg sync.WaitGroup

		for range 10 {
			wg.Go(func() {
				val, err := cache.GetOrSet(ctx, c, "dedup", func(_ context.Context) (int, time.Duration, error) {
					calls.Add(1)
					time.Sleep(10 * time.Millisecond) // Simulate slow computation.
					return 42, time.Minute, nil
				})
				require.NoError(t, err)
				require.Equal(t, 42, val)
			})
		}

		wg.Wait()

		require.LessOrEqual(t, calls.Load(), int64(2),
			"fn should be called at most twice due to singleflight dedup")
	})
}
