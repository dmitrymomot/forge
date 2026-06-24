package redis

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestOpen_Validation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("empty URL returns ErrEmptyConnectionURL", func(t *testing.T) {
		t.Parallel()

		client, err := Open(ctx, Config{})
		require.Error(t, err)
		require.Nil(t, client)
		require.True(t, errors.Is(err, ErrEmptyConnectionURL))
	})

	t.Run("invalid scheme returns ErrFailedToParseURL", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name string
			url  string
		}{
			{
				name: "http scheme",
				url:  "http://localhost:6379",
			},
			{
				name: "https scheme",
				url:  "https://localhost:6379",
			},
			{
				name: "no scheme",
				url:  "localhost:6379",
			},
			{
				name: "postgresql scheme",
				url:  "postgresql://localhost:6379",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				client, err := Open(ctx, Config{URL: tc.url})
				require.Error(t, err)
				require.Nil(t, client)
				require.True(t, errors.Is(err, ErrFailedToParseURL))
			})
		}
	})

	t.Run("malformed URL returns ErrFailedToParseURL", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name string
			url  string
		}{
			{
				name: "invalid port",
				url:  "redis://localhost:notaport",
			},
			{
				name: "invalid database",
				url:  "redis://localhost:6379/notanumber",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				client, err := Open(ctx, Config{URL: tc.url})
				require.Error(t, err)
				require.Nil(t, client)
				require.True(t, errors.Is(err, ErrFailedToParseURL))
			})
		}
	})
}

func TestHealthcheck_NilClient(t *testing.T) {
	t.Parallel()

	t.Run("nil client returns ErrHealthcheckFailed", func(t *testing.T) {
		t.Parallel()

		check := Healthcheck(nil)
		err := check(context.Background())
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrHealthcheckFailed))
	})
}

func TestShutdown_MockCloser(t *testing.T) {
	t.Parallel()

	t.Run("calls Close on the client", func(t *testing.T) {
		t.Parallel()

		mockCloser := &mockCloser{}
		shutdown := Shutdown(mockCloser)

		err := shutdown(context.Background())
		require.NoError(t, err)
		require.True(t, mockCloser.closed)
	})

	t.Run("propagates Close error", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("close error")
		mockCloser := &mockCloser{err: expectedErr}
		shutdown := Shutdown(mockCloser)

		err := shutdown(context.Background())
		require.Error(t, err)
		require.Equal(t, expectedErr, err)
		require.True(t, mockCloser.closed)
	})
}

func TestWait_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("cancelled context returns immediately", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		start := time.Now()
		err := wait(ctx, 10*time.Second)
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Equal(t, context.Canceled, err)
		require.Less(t, elapsed, 1*time.Second, "should return immediately")
	})

	t.Run("timeout completes normally", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		duration := 50 * time.Millisecond

		start := time.Now()
		err := wait(ctx, duration)
		elapsed := time.Since(start)

		require.NoError(t, err)
		require.GreaterOrEqual(t, elapsed, duration, "should wait for the full duration")
	})

	t.Run("context cancelled during wait", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		err := wait(ctx, 10*time.Second)
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Equal(t, context.Canceled, err)
		require.Less(t, elapsed, 1*time.Second, "should return when context is cancelled")
		require.GreaterOrEqual(t, elapsed, 50*time.Millisecond, "should wait until cancellation")
	})
}

func TestConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	t.Run("zero value config gets defaults", func(t *testing.T) {
		t.Parallel()

		cfg := Config{URL: "redis://localhost:6379"}
		cfg.applyDefaults()
		require.Equal(t, 10, cfg.PoolSize)
		require.Equal(t, 5, cfg.MinIdleConns)
		require.Equal(t, 10*time.Minute, cfg.MaxIdleTime)
		require.Equal(t, 30*time.Minute, cfg.MaxActiveTime)
		require.Equal(t, 3, cfg.RetryAttempts)
		require.Equal(t, 5*time.Second, cfg.RetryInterval)
		require.Equal(t, 3*time.Second, cfg.ReadTimeout)
		require.Equal(t, 3*time.Second, cfg.WriteTimeout)
		require.Equal(t, 5*time.Second, cfg.DialTimeout)
	})

	t.Run("explicit values are preserved", func(t *testing.T) {
		t.Parallel()

		cfg := Config{
			URL:           "redis://localhost:6379",
			PoolSize:      25,
			MinIdleConns:  10,
			MaxIdleTime:   15 * time.Minute,
			MaxActiveTime: 45 * time.Minute,
			RetryAttempts: 5,
			RetryInterval: 10 * time.Second,
			ReadTimeout:   7 * time.Second,
			WriteTimeout:  8 * time.Second,
			DialTimeout:   10 * time.Second,
		}
		cfg.applyDefaults()

		require.Equal(t, 25, cfg.PoolSize)
		require.Equal(t, 10, cfg.MinIdleConns)
		require.Equal(t, 15*time.Minute, cfg.MaxIdleTime)
		require.Equal(t, 45*time.Minute, cfg.MaxActiveTime)
		require.Equal(t, 5, cfg.RetryAttempts)
		require.Equal(t, 10*time.Second, cfg.RetryInterval)
		require.Equal(t, 7*time.Second, cfg.ReadTimeout)
		require.Equal(t, 8*time.Second, cfg.WriteTimeout)
		require.Equal(t, 10*time.Second, cfg.DialTimeout)
	})
}

func TestOpen_HappyPath(t *testing.T) {
	t.Parallel()

	t.Run("connects to a live server and passes healthcheck", func(t *testing.T) {
		t.Parallel()

		s := miniredis.RunT(t)

		client, err := Open(context.Background(), Config{URL: "redis://" + s.Addr()})
		require.NoError(t, err)
		require.NotNil(t, client)
		t.Cleanup(func() { _ = client.Close() })

		// The returned client must be live: a real SET/GET round-trips through miniredis.
		require.NoError(t, client.Set(context.Background(), "k", "v", 0).Err())
		got, err := client.Get(context.Background(), "k").Result()
		require.NoError(t, err)
		require.Equal(t, "v", got)

		// Healthcheck against the same live client succeeds.
		require.NoError(t, Healthcheck(client)(context.Background()))
	})

	t.Run("applies defaults to the underlying pool options", func(t *testing.T) {
		t.Parallel()

		s := miniredis.RunT(t)

		client, err := Open(context.Background(), Config{URL: "redis://" + s.Addr()})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		// Open returns a single-node client; inspect its applied options.
		nodeClient, ok := client.(*goredis.Client)
		require.True(t, ok, "Open should construct a single-node *redis.Client")
		opts := nodeClient.Options()
		require.Equal(t, defaultPoolSize, opts.PoolSize)
		require.Equal(t, defaultMinIdleConns, opts.MinIdleConns)
		require.Equal(t, defaultMaxIdleTime, opts.ConnMaxIdleTime)
		require.Equal(t, defaultDialTimeout, opts.DialTimeout)
	})
}

func TestConnect_RetryBackoff(t *testing.T) {
	t.Parallel()

	// optsFor builds redis options pointing at a (possibly closed) address with
	// fast dial/read/write timeouts so failing attempts return quickly.
	const dialTimeout = 100 * time.Millisecond
	optsFor := func(addr string) *goredis.Options {
		return &goredis.Options{
			Addr:         addr,
			DialTimeout:  dialTimeout,
			ReadTimeout:  dialTimeout,
			WriteTimeout: dialTimeout,
			// Disable go-redis's internal per-call retries so each Ping performs a
			// single dial. With the default (MaxRetries: 3) one Ping against a dead
			// address fans out into several dials with its own backoff, adding
			// non-deterministic latency that makes the timing assertions below flaky
			// under load and the race detector.
			MaxRetries: -1,
		}
	}

	t.Run("succeeds without sleeping when first ping works", func(t *testing.T) {
		t.Parallel()

		s := miniredis.RunT(t)

		start := time.Now()
		client, err := connect(context.Background(), optsFor(s.Addr()), 3, time.Second)
		elapsed := time.Since(start)

		require.NoError(t, err)
		require.NotNil(t, client)
		t.Cleanup(func() { _ = client.Close() })
		require.Less(t, elapsed, 500*time.Millisecond, "happy path must not sleep a backoff interval")
	})

	t.Run("does not wait a backoff interval after the final failed attempt", func(t *testing.T) {
		t.Parallel()

		// Bind a server then close it so all pings fail fast against a dead address.
		s := miniredis.RunT(t)
		addr := s.Addr()
		s.Close()

		const attempts = 3
		const interval = 300 * time.Millisecond

		start := time.Now()
		client, err := connect(context.Background(), optsFor(addr), attempts, interval)
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Nil(t, client)

		// With the trailing-wait fix, only the waits BETWEEN attempts run:
		// for 3 attempts that is interval*1 + interval*2 = 3*interval = 900ms. The
		// buggy version would additionally sleep interval*3 = 900ms after the last
		// attempt. Each failed Ping also costs up to one dialTimeout (internal
		// retries are disabled via MaxRetries:-1), so budget that in. The ceiling
		// sits midway between the fixed and buggy wait totals, comfortably clear of
		// dial jitter on either side (~450ms of margin each way).
		interAttemptWaits := time.Duration(attempts) * interval // 900ms
		dialBudget := time.Duration(attempts) * dialTimeout     // up to 300ms
		trailingWait := time.Duration(attempts) * interval      // 900ms the bug would add
		ceiling := interAttemptWaits + dialBudget + trailingWait/2
		require.Less(t, elapsed, ceiling, "must not sleep after the final attempt")
	})

	t.Run("joins ErrConnectionFailed with the underlying ping error", func(t *testing.T) {
		t.Parallel()

		s := miniredis.RunT(t)
		addr := s.Addr()
		s.Close()

		// Single attempt: fails immediately, no backoff wait at all.
		start := time.Now()
		client, err := connect(context.Background(), optsFor(addr), 1, time.Second)
		elapsed := time.Since(start)

		require.Error(t, err)
		require.Nil(t, client)
		require.True(t, errors.Is(err, ErrConnectionFailed), "must expose the sentinel")

		// The underlying cause must be reachable, not discarded: unwrapping past
		// the sentinel must yield a non-nil cause distinct from the sentinel.
		require.NotErrorIs(t, errors.Unwrap(err), ErrConnectionFailed)
		require.Less(t, elapsed, 500*time.Millisecond, "single attempt must not sleep")
	})

	t.Run("returns context error when cancelled mid-backoff", func(t *testing.T) {
		t.Parallel()

		s := miniredis.RunT(t)
		addr := s.Addr()
		s.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		client, err := connect(ctx, optsFor(addr), 5, time.Second)
		require.Error(t, err)
		require.Nil(t, client)
		require.True(t, errors.Is(err, ErrConnectionFailed))
		require.True(t, errors.Is(err, context.Canceled), "cancellation cause must be exposed")
	})
}

func TestOpen_ConnectionFailure(t *testing.T) {
	t.Parallel()

	t.Run("unreachable server returns joined ErrConnectionFailed", func(t *testing.T) {
		t.Parallel()

		s := miniredis.RunT(t)
		addr := s.Addr()
		s.Close()

		cfg := Config{
			URL:           "redis://" + addr,
			RetryAttempts: 1,
			DialTimeout:   100 * time.Millisecond,
			ReadTimeout:   100 * time.Millisecond,
			WriteTimeout:  100 * time.Millisecond,
		}

		client, err := Open(context.Background(), cfg)
		require.Error(t, err)
		require.Nil(t, client)
		require.True(t, errors.Is(err, ErrConnectionFailed))
		require.NotErrorIs(t, errors.Unwrap(err), ErrConnectionFailed, "underlying cause must be preserved")
	})
}

// mockCloser is a test double for io.Closer
type mockCloser struct {
	closed bool
	err    error
}

func (m *mockCloser) Close() error {
	m.closed = true
	return m.err
}

var _ io.Closer = (*mockCloser)(nil)
