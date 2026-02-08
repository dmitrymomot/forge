package ratelimit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/ratelimit"
)

// --- Test helpers ---

func newTestRedis(t *testing.T) (goredis.UniversalClient, *miniredis.Miniredis) {
	t.Helper()
	s := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client, s
}

func newMemoryCounter(t *testing.T) *ratelimit.MemoryCounter {
	t.Helper()
	c := ratelimit.NewMemoryCounter(ratelimit.MemoryConfig{CleanupInterval: -1})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// --- Limiter: New ---

func TestLimiter_New(t *testing.T) {
	t.Parallel()

	t.Run("returns ErrNilCounter for nil counter", func(t *testing.T) {
		t.Parallel()

		_, err := ratelimit.New(nil, 100, time.Minute)
		require.ErrorIs(t, err, ratelimit.ErrNilCounter)
	})

	t.Run("returns ErrInvalidLimit for zero limit", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		_, err := ratelimit.New(counter, 0, time.Minute)
		require.ErrorIs(t, err, ratelimit.ErrInvalidLimit)
	})

	t.Run("returns ErrInvalidLimit for negative limit", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		_, err := ratelimit.New(counter, -5, time.Minute)
		require.ErrorIs(t, err, ratelimit.ErrInvalidLimit)
	})

	t.Run("returns ErrInvalidWindow for zero window", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		_, err := ratelimit.New(counter, 100, 0)
		require.ErrorIs(t, err, ratelimit.ErrInvalidWindow)
	})

	t.Run("returns ErrInvalidWindow for negative window", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		_, err := ratelimit.New(counter, 100, -time.Second)
		require.ErrorIs(t, err, ratelimit.ErrInvalidWindow)
	})

	t.Run("succeeds with valid arguments", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 100, time.Minute)
		require.NoError(t, err)
		require.NotNil(t, lim)
	})
}

// --- Limiter: Allow ---

func TestLimiter_Allow(t *testing.T) {
	t.Parallel()

	t.Run("allows first request", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 10, time.Minute)
		require.NoError(t, err)

		info, err := lim.Allow(context.Background(), "key")
		require.NoError(t, err)
		require.True(t, info.IsAllowed())
		require.Equal(t, int64(10), info.Limit)
		require.Equal(t, int64(9), info.Remaining)
		require.Zero(t, info.RetryAfter)
	})

	t.Run("decrements remaining on each request", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 5, time.Minute)
		require.NoError(t, err)

		ctx := context.Background()
		for i := range 4 {
			info, err := lim.Allow(ctx, "key")
			require.NoError(t, err)
			require.Equal(t, int64(4-i), info.Remaining)
		}
	})

	t.Run("rate limits when limit exceeded", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 3, time.Minute)
		require.NoError(t, err)

		ctx := context.Background()
		for range 3 {
			_, err := lim.Allow(ctx, "key")
			require.NoError(t, err)
		}

		info, err := lim.Allow(ctx, "key")
		require.NoError(t, err)
		require.False(t, info.IsAllowed())
		require.Equal(t, int64(0), info.Remaining)
		require.Greater(t, info.RetryAfter, time.Duration(0))
	})

	t.Run("isolates different keys", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 2, time.Minute)
		require.NoError(t, err)

		ctx := context.Background()

		// Exhaust key-a
		for range 2 {
			_, err := lim.Allow(ctx, "key-a")
			require.NoError(t, err)
		}

		// key-b should still be allowed
		info, err := lim.Allow(ctx, "key-b")
		require.NoError(t, err)
		require.True(t, info.IsAllowed())
		require.Equal(t, int64(1), info.Remaining)
	})
}

// --- Limiter: AllowN ---

func TestLimiter_AllowN(t *testing.T) {
	t.Parallel()

	t.Run("allows batch within limit", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 10, time.Minute)
		require.NoError(t, err)

		info, err := lim.AllowN(context.Background(), "key", 5)
		require.NoError(t, err)
		require.True(t, info.IsAllowed())
		require.Equal(t, int64(5), info.Remaining)
	})

	t.Run("rejects batch exceeding limit", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 10, time.Minute)
		require.NoError(t, err)

		info, err := lim.AllowN(context.Background(), "key", 11)
		require.NoError(t, err)
		require.False(t, info.IsAllowed())
		require.Equal(t, int64(0), info.Remaining)
	})
}

// --- Limiter: Peek ---

func TestLimiter_Peek(t *testing.T) {
	t.Parallel()

	t.Run("does not increment counter", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 10, time.Minute)
		require.NoError(t, err)

		ctx := context.Background()

		// Peek should not change remaining
		info1, err := lim.Peek(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, int64(10), info1.Remaining)

		info2, err := lim.Peek(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, int64(10), info2.Remaining)
	})

	t.Run("reflects current usage", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 10, time.Minute)
		require.NoError(t, err)

		ctx := context.Background()

		_, err = lim.AllowN(ctx, "key", 7)
		require.NoError(t, err)

		info, err := lim.Peek(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, int64(3), info.Remaining)
	})
}

// --- Limiter: Sliding Window ---

func TestLimiter_SlidingWindow(t *testing.T) {
	t.Parallel()

	t.Run("blends previous and current window", func(t *testing.T) {
		t.Parallel()

		// Use the counter directly to set up a deterministic previous window,
		// then test the Limiter's sliding window blend.
		counter := newMemoryCounter(t)
		window := time.Second
		lim, err := ratelimit.New(counter, 100, window)
		require.NoError(t, err)

		ctx := context.Background()

		// Seed the previous window with 80 requests by writing directly to
		// the counter at the previous window's timestamp.
		prevWindow := time.Now().Truncate(window).Add(-window)
		_, err = counter.Increment(ctx, "key", prevWindow, 2*window, 80)
		require.NoError(t, err)

		// Peek at the current state. Since we're somewhere in the current
		// window, the previous window's 80 requests are partially weighted.
		// The remaining should be between 20 (weight=1.0) and 100 (weight=0.0).
		info, err := lim.Peek(ctx, "key")
		require.NoError(t, err)
		require.Greater(t, info.Remaining, int64(20))
		require.LessOrEqual(t, info.Remaining, int64(100))
	})

	t.Run("previous window fully decays after two windows", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		window := 150 * time.Millisecond
		lim, err := ratelimit.New(counter, 100, window)
		require.NoError(t, err)

		ctx := context.Background()

		// Seed the previous window with 100 requests.
		prevWindow := time.Now().Truncate(window).Add(-window)
		_, err = counter.Increment(ctx, "key", prevWindow, 2*window, 100)
		require.NoError(t, err)

		// Wait for two full windows to pass — previous window data should be
		// expired (TTL = 2*window) and the current window is empty.
		time.Sleep(2*window + 50*time.Millisecond)

		info, err := lim.Peek(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, int64(100), info.Remaining)
	})
}

// --- Limiter: Info ---

func TestInfo_IsAllowed(t *testing.T) {
	t.Parallel()

	t.Run("true when RetryAfter is zero", func(t *testing.T) {
		t.Parallel()

		info := ratelimit.Info{RetryAfter: 0}
		require.True(t, info.IsAllowed())
	})

	t.Run("false when RetryAfter is positive", func(t *testing.T) {
		t.Parallel()

		info := ratelimit.Info{RetryAfter: time.Second}
		require.False(t, info.IsAllowed())
	})
}

// --- Limiter: ResetAt ---

func TestLimiter_ResetAt(t *testing.T) {
	t.Parallel()

	t.Run("ResetAt is in the future", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		lim, err := ratelimit.New(counter, 10, time.Minute)
		require.NoError(t, err)

		info, err := lim.Allow(context.Background(), "key")
		require.NoError(t, err)
		require.True(t, info.ResetAt.After(time.Now()))
		require.True(t, info.ResetAt.Before(time.Now().Add(time.Minute+time.Second)))
	})
}

// --- MemoryCounter ---

func TestMemoryCounter_Increment(t *testing.T) {
	t.Parallel()

	t.Run("returns count after increment", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		count, err := counter.Increment(ctx, "key", window, 2*time.Minute, 1)
		require.NoError(t, err)
		require.Equal(t, int64(1), count)

		count, err = counter.Increment(ctx, "key", window, 2*time.Minute, 1)
		require.NoError(t, err)
		require.Equal(t, int64(2), count)
	})

	t.Run("increments by n", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		count, err := counter.Increment(ctx, "key", window, 2*time.Minute, 5)
		require.NoError(t, err)
		require.Equal(t, int64(5), count)
	})

	t.Run("isolates different windows", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		ctx := context.Background()
		window1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		window2 := time.Date(2025, 1, 1, 0, 1, 0, 0, time.UTC)

		_, err := counter.Increment(ctx, "key", window1, 5*time.Minute, 10)
		require.NoError(t, err)

		count, err := counter.Increment(ctx, "key", window2, 5*time.Minute, 3)
		require.NoError(t, err)
		require.Equal(t, int64(3), count)
	})
}

func TestMemoryCounter_Get(t *testing.T) {
	t.Parallel()

	t.Run("returns 0 for missing window", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		window := time.Now().Truncate(time.Minute)

		count, err := counter.Get(context.Background(), "missing", window)
		require.NoError(t, err)
		require.Equal(t, int64(0), count)
	})

	t.Run("returns stored count", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		_, err := counter.Increment(ctx, "key", window, 2*time.Minute, 7)
		require.NoError(t, err)

		count, err := counter.Get(ctx, "key", window)
		require.NoError(t, err)
		require.Equal(t, int64(7), count)
	})

	t.Run("returns 0 for expired window", func(t *testing.T) {
		t.Parallel()

		counter := newMemoryCounter(t)
		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		// Set with a very short TTL
		_, err := counter.Increment(ctx, "key", window, time.Millisecond, 5)
		require.NoError(t, err)

		time.Sleep(5 * time.Millisecond)

		count, err := counter.Get(ctx, "key", window)
		require.NoError(t, err)
		require.Equal(t, int64(0), count)
	})
}

func TestMemoryCounter_Close(t *testing.T) {
	t.Parallel()

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()

		counter := ratelimit.NewMemoryCounter(ratelimit.MemoryConfig{CleanupInterval: -1})
		require.NoError(t, counter.Close())
		require.NoError(t, counter.Close())
	})
}

func TestMemoryCounter_Cleanup(t *testing.T) {
	t.Parallel()

	t.Run("janitor removes expired entries", func(t *testing.T) {
		t.Parallel()

		counter := ratelimit.NewMemoryCounter(ratelimit.MemoryConfig{
			CleanupInterval: 50 * time.Millisecond,
		})
		defer counter.Close()

		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		_, err := counter.Increment(ctx, "key", window, 50*time.Millisecond, 5)
		require.NoError(t, err)

		// Wait for TTL + cleanup interval
		time.Sleep(150 * time.Millisecond)

		count, err := counter.Get(ctx, "key", window)
		require.NoError(t, err)
		require.Equal(t, int64(0), count)
	})
}

func TestMemoryCounter_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	counter := newMemoryCounter(t)
	ctx := context.Background()
	window := time.Now().Truncate(time.Minute)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			_, _ = counter.Increment(ctx, "key", window, 2*time.Minute, 1)
		}()
	}
	wg.Wait()

	count, err := counter.Get(ctx, "key", window)
	require.NoError(t, err)
	require.Equal(t, int64(goroutines), count)
}

// --- RedisCounter ---

func TestRedisCounter_Increment(t *testing.T) {
	t.Parallel()

	t.Run("returns count after increment", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		counter := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{Prefix: "test-incr"})

		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		count, err := counter.Increment(ctx, "key", window, 2*time.Minute, 1)
		require.NoError(t, err)
		require.Equal(t, int64(1), count)

		count, err = counter.Increment(ctx, "key", window, 2*time.Minute, 1)
		require.NoError(t, err)
		require.Equal(t, int64(2), count)
	})

	t.Run("increments by n", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		counter := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{Prefix: "test-incr-n"})

		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		count, err := counter.Increment(ctx, "key", window, 2*time.Minute, 5)
		require.NoError(t, err)
		require.Equal(t, int64(5), count)
	})
}

func TestRedisCounter_Get(t *testing.T) {
	t.Parallel()

	t.Run("returns 0 for missing window", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		counter := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{Prefix: "test-get-miss"})

		window := time.Now().Truncate(time.Minute)
		count, err := counter.Get(context.Background(), "missing", window)
		require.NoError(t, err)
		require.Equal(t, int64(0), count)
	})

	t.Run("returns stored count", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		counter := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{Prefix: "test-get-hit"})

		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		_, err := counter.Increment(ctx, "key", window, 2*time.Minute, 7)
		require.NoError(t, err)

		count, err := counter.Get(ctx, "key", window)
		require.NoError(t, err)
		require.Equal(t, int64(7), count)
	})

	t.Run("returns 0 for expired key", func(t *testing.T) {
		t.Parallel()

		client, s := newTestRedis(t)
		counter := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{Prefix: "test-get-exp"})

		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		_, err := counter.Increment(ctx, "key", window, time.Second, 5)
		require.NoError(t, err)

		s.FastForward(2 * time.Second)

		count, err := counter.Get(ctx, "key", window)
		require.NoError(t, err)
		require.Equal(t, int64(0), count)
	})
}

func TestRedisCounter_KeyPrefix(t *testing.T) {
	t.Parallel()

	t.Run("different prefixes are isolated", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		counter1 := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{Prefix: "prefix-a"})
		counter2 := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{Prefix: "prefix-b"})

		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		_, err := counter1.Increment(ctx, "key", window, 2*time.Minute, 10)
		require.NoError(t, err)

		count, err := counter2.Get(ctx, "key", window)
		require.NoError(t, err)
		require.Equal(t, int64(0), count)
	})
}

func TestRedisCounter_Fallback(t *testing.T) {
	t.Parallel()

	t.Run("falls back to memory on Redis failure", func(t *testing.T) {
		t.Parallel()

		client, s := newTestRedis(t)
		counter := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{Prefix: "test-fallback"})
		defer counter.Close()

		ctx := context.Background()
		window := time.Now().Truncate(time.Minute)

		// Close Redis to simulate failure
		s.Close()

		// Should fall back to memory counter
		count, err := counter.Increment(ctx, "key", window, 2*time.Minute, 3)
		require.NoError(t, err)
		require.Equal(t, int64(3), count)

		count, err = counter.Get(ctx, "key", window)
		require.NoError(t, err)
		require.Equal(t, int64(3), count)
	})
}

// --- Limiter with RedisCounter ---

func TestLimiter_WithRedisCounter(t *testing.T) {
	t.Parallel()

	t.Run("rate limits through Redis", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestRedis(t)
		counter := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{Prefix: "test-lim"})

		lim, err := ratelimit.New(counter, 3, time.Minute)
		require.NoError(t, err)

		ctx := context.Background()
		for range 3 {
			info, err := lim.Allow(ctx, "key")
			require.NoError(t, err)
			require.True(t, info.IsAllowed())
		}

		info, err := lim.Allow(ctx, "key")
		require.NoError(t, err)
		require.False(t, info.IsAllowed())
	})
}

// --- KeyFunc ---

func TestKeyByIP(t *testing.T) {
	t.Parallel()

	t.Run("extracts IP from request", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "192.168.1.1:12345"

		key := ratelimit.KeyByIP(r)
		require.Equal(t, "192.168.1.1", key)
	})

	t.Run("prefers X-Forwarded-For", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.1:12345"
		r.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18")

		key := ratelimit.KeyByIP(r)
		require.Equal(t, "203.0.113.50", key)
	})
}

func TestKeyByPath(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	key := ratelimit.KeyByPath(r)
	require.Equal(t, "/api/users", key)
}

func TestKeyByHeader(t *testing.T) {
	t.Parallel()

	t.Run("extracts header value", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-API-Key", "abc123")

		keyFn := ratelimit.KeyByHeader("X-API-Key")
		require.Equal(t, "abc123", keyFn(r))
	})

	t.Run("returns empty for missing header", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		keyFn := ratelimit.KeyByHeader("X-API-Key")
		require.Equal(t, "", keyFn(r))
	})
}

func TestKeyComposite(t *testing.T) {
	t.Parallel()

	t.Run("combines multiple extractors", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		r.RemoteAddr = "192.168.1.1:12345"

		keyFn := ratelimit.KeyComposite(ratelimit.KeyByIP, ratelimit.KeyByPath)
		key := keyFn(r)
		require.Equal(t, "192.168.1.1:/api/users", key)
	})

	t.Run("skips empty values", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		r.RemoteAddr = "192.168.1.1:12345"

		emptyHeader := ratelimit.KeyByHeader("X-Missing")
		keyFn := ratelimit.KeyComposite(ratelimit.KeyByIP, emptyHeader, ratelimit.KeyByPath)
		key := keyFn(r)
		require.Equal(t, "192.168.1.1:/api/users", key)
	})
}

func TestKeyByFingerprint(t *testing.T) {
	t.Parallel()

	t.Run("returns non-empty fingerprint", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("User-Agent", "Mozilla/5.0")
		r.Header.Set("Accept", "text/html")
		r.Header.Set("Accept-Language", "en-US")

		key := ratelimit.KeyByFingerprint(r)
		require.NotEmpty(t, key)
	})
}
