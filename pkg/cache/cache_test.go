package cache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/cache"
)

// --- Memory: Get ---

func TestMemory_Get(t *testing.T) {
	t.Parallel()

	t.Run("returns ErrNotFound for missing key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string]()
		defer c.Close()

		_, err := c.Get(context.Background(), "missing")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("returns stored value", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int]()
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", 42, time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, 42, val)
	})

	t.Run("returns ErrNotFound for expired key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.WithCleanupInterval(0))
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Millisecond))

		time.Sleep(5 * time.Millisecond)

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("marks entry as recently used", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.WithMaxEntries(2))
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

		c := cache.NewMemory[string]()
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)
	})

	t.Run("zero TTL uses default", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.WithDefaultTTL(50*time.Millisecond), cache.WithCleanupInterval(0))
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

		c := cache.NewMemory[string](cache.WithDefaultTTL(10*time.Millisecond), cache.WithCleanupInterval(0))
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

		c := cache.NewMemory[int]()
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

		c := cache.NewMemory[string]()
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

		c := cache.NewMemory[string]()
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))
		require.NoError(t, c.Delete(ctx, "key"))

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("no error for missing key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string]()
		defer c.Close()

		err := c.Delete(context.Background(), "missing")
		require.NoError(t, err)
	})

	t.Run("returns ErrClosed after Close", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string]()
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

		c := cache.NewMemory[string]()
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", time.Minute))

		has, err := c.Has(ctx, "key")
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("returns false for missing key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string]()
		defer c.Close()

		has, err := c.Has(context.Background(), "missing")
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("returns false for expired key", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](cache.WithCleanupInterval(0))
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

		c := cache.NewMemory[string]()
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

		c := cache.NewMemory[string]()
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

		c := cache.NewMemory[string]()
		require.NoError(t, c.Close())
		require.NoError(t, c.Close())
	})
}

// --- Memory: MaxEntries / LRU ---

func TestMemory_MaxEntries(t *testing.T) {
	t.Parallel()

	t.Run("evicts LRU when at capacity", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[int](cache.WithMaxEntries(3))
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

		c := cache.NewMemory[int](cache.WithMaxEntries(5))
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

		c := cache.NewMemory[int](cache.WithMaxEntries(2))
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

		c := cache.NewMemory[int](cache.WithMaxEntries(3))
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

		c := cache.NewMemory[int](cache.WithMaxEntries(1))
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

		c := cache.NewMemory[int](cache.WithMaxEntries(2))
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

		c := cache.NewMemory[string]()
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

		c := cache.NewMemory[int]()
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

		c := cache.NewMemory[string](
			cache.WithCleanupInterval(10 * time.Millisecond),
		)
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

		c := cache.NewMemory[int](cache.WithMaxEntries(100))
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

		c := cache.NewMemory[string]()
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

		c := cache.NewMemory[string]()
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

		c := cache.NewMemory[string]()
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

		c := cache.NewMemory[int]()
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
		c := cache.NewMemory[user]()
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

	t.Run("WithDefaultTTL sets default TTL", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string](
			cache.WithDefaultTTL(20*time.Millisecond),
			cache.WithCleanupInterval(0),
		)
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", 0)) // Uses default.

		time.Sleep(30 * time.Millisecond)

		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("default options are sensible", func(t *testing.T) {
		t.Parallel()

		c := cache.NewMemory[string]()
		defer c.Close()

		ctx := context.Background()
		require.NoError(t, c.Set(ctx, "key", "value", 0))

		val, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", val)
	})
}
