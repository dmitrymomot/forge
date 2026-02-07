package redis

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpen_Validation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("empty URL returns ErrEmptyConnectionURL", func(t *testing.T) {
		t.Parallel()

		client, err := Open(ctx, "")
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

				client, err := Open(ctx, tc.url)
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

				client, err := Open(ctx, tc.url)
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

func TestOptions(t *testing.T) {
	t.Parallel()

	t.Run("default options", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		require.Equal(t, 10, opts.poolSize)
		require.Equal(t, 5, opts.minIdleConns)
		require.Equal(t, 10*time.Minute, opts.maxIdleTime)
		require.Equal(t, 30*time.Minute, opts.maxActiveTime)
		require.Equal(t, 3, opts.retryAttempts)
		require.Equal(t, 5*time.Second, opts.retryInterval)
		require.Equal(t, 3*time.Second, opts.readTimeout)
		require.Equal(t, 3*time.Second, opts.writeTimeout)
		require.Equal(t, 5*time.Second, opts.dialTimeout)
	})

	t.Run("WithPoolSize sets pool size", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		WithPoolSize(25)(opts)
		require.Equal(t, 25, opts.poolSize)
	})

	t.Run("WithMinIdleConns sets min idle connections", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		WithMinIdleConns(10)(opts)
		require.Equal(t, 10, opts.minIdleConns)
	})

	t.Run("WithMaxIdleTime sets max idle time", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		WithMaxIdleTime(15 * time.Minute)(opts)
		require.Equal(t, 15*time.Minute, opts.maxIdleTime)
	})

	t.Run("WithMaxActiveTime sets max active time", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		WithMaxActiveTime(45 * time.Minute)(opts)
		require.Equal(t, 45*time.Minute, opts.maxActiveTime)
	})

	t.Run("WithRetry sets retry attempts and interval", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		WithRetry(5, 10*time.Second)(opts)
		require.Equal(t, 5, opts.retryAttempts)
		require.Equal(t, 10*time.Second, opts.retryInterval)
	})

	t.Run("WithReadTimeout sets read timeout", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		WithReadTimeout(7 * time.Second)(opts)
		require.Equal(t, 7*time.Second, opts.readTimeout)
	})

	t.Run("WithWriteTimeout sets write timeout", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		WithWriteTimeout(8 * time.Second)(opts)
		require.Equal(t, 8*time.Second, opts.writeTimeout)
	})

	t.Run("WithDialTimeout sets dial timeout", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		WithDialTimeout(10 * time.Second)(opts)
		require.Equal(t, 10*time.Second, opts.dialTimeout)
	})

	t.Run("multiple options applied in order", func(t *testing.T) {
		t.Parallel()

		opts := defaultOptions()
		WithPoolSize(20)(opts)
		WithMinIdleConns(8)(opts)
		WithRetry(7, 2*time.Second)(opts)

		require.Equal(t, 20, opts.poolSize)
		require.Equal(t, 8, opts.minIdleConns)
		require.Equal(t, 7, opts.retryAttempts)
		require.Equal(t, 2*time.Second, opts.retryInterval)
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
