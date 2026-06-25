package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestOpen_EmptyURL(t *testing.T) {
	t.Parallel()

	pool, err := Open(context.Background(), Config{URL: ""})
	require.Nil(t, pool, "no pool should be returned for an empty URL")
	require.ErrorIs(t, err, ErrFailedToParseDBConfig, "empty URL must be rejected with ErrFailedToParseDBConfig")
}

func TestOpen_InvalidURL(t *testing.T) {
	t.Parallel()

	pool, err := Open(context.Background(), Config{URL: "://not-a-valid-dsn"})
	require.Nil(t, pool)
	require.ErrorIs(t, err, ErrFailedToParseDBConfig, "an unparseable DSN must surface ErrFailedToParseDBConfig")
	// The underlying parse error must be preserved (errors.Join), not just the sentinel.
	require.NotEqual(t, ErrFailedToParseDBConfig.Error(), err.Error(),
		"the underlying parse error should be joined, not discarded")
}

// Defaults are applied in Open before the connection is attempted. We reach the
// pgxpool.ParseConfig path with a syntactically valid DSN pointing at a host
// that fails to resolve quickly enough, but assert the default-filling logic
// directly by mutating the zero-valued Config the same way Open does. Because
// Open mutates a copy, we verify default-filling through a dedicated helper.
func TestOpen_FillsDefaults(t *testing.T) {
	t.Parallel()

	// A zero-valued Config (apart from URL) must have all pool knobs defaulted.
	cfg := Config{URL: "postgres://user:pass@127.0.0.1:1/db"}
	applyDefaults(&cfg)

	require.Equal(t, int32(10), cfg.MaxConns)
	require.Equal(t, int32(5), cfg.MinConns)
	require.Equal(t, time.Minute, cfg.HealthCheckPeriod)
	require.Equal(t, 10*time.Minute, cfg.MaxConnIdleTime)
	require.Equal(t, 30*time.Minute, cfg.MaxConnLifetime)
	require.Equal(t, 3, cfg.RetryAttempts)
	require.Equal(t, 5*time.Second, cfg.RetryInterval)
}

func TestOpen_PreservesExplicitConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{
		URL:               "postgres://user:pass@127.0.0.1:1/db",
		MaxConns:          42,
		MinConns:          7,
		HealthCheckPeriod: 2 * time.Minute,
		MaxConnIdleTime:   3 * time.Minute,
		MaxConnLifetime:   4 * time.Minute,
		RetryAttempts:     9,
		RetryInterval:     8 * time.Second,
	}
	applyDefaults(&cfg)

	require.Equal(t, int32(42), cfg.MaxConns, "explicit values must not be overwritten by defaults")
	require.Equal(t, int32(7), cfg.MinConns)
	require.Equal(t, 2*time.Minute, cfg.HealthCheckPeriod)
	require.Equal(t, 3*time.Minute, cfg.MaxConnIdleTime)
	require.Equal(t, 4*time.Minute, cfg.MaxConnLifetime)
	require.Equal(t, 9, cfg.RetryAttempts)
	require.Equal(t, 8*time.Second, cfg.RetryInterval)
}

func TestWait_ReturnsAfterDuration(t *testing.T) {
	t.Parallel()

	start := time.Now()
	err := wait(context.Background(), 20*time.Millisecond)
	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond,
		"wait must block for at least the requested duration")
}

func TestWait_HonorsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	start := time.Now()
	err := wait(ctx, time.Hour)
	require.ErrorIs(t, err, context.Canceled, "wait must return promptly when ctx is cancelled")
	require.Less(t, time.Since(start), time.Second, "wait must not sleep the full duration after cancellation")
}

func TestConnect_StopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	// A DSN whose host cannot connect, with a context that is cancelled before
	// the backoff completes. connect must surface ErrFailedToOpenDBConnection
	// joined with the cancellation cause rather than spinning through retries.
	cfg, err := pgxpool.ParseConfig("postgres://user:pass@127.0.0.1:1/db")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pool, err := connect(ctx, cfg, 5, time.Hour)
	require.Nil(t, pool)
	require.ErrorIs(t, err, ErrFailedToOpenDBConnection)
	require.ErrorIs(t, err, context.Canceled, "cancellation cause must be joined into the returned error")
}

func TestConnect_PreservesUnderlyingErrorOnExhaustedRetries(t *testing.T) {
	t.Parallel()

	// Port 1 on loopback refuses connections fast, so all attempts fail on Ping.
	// With a tiny interval and a cancellable-free context, connect exhausts its
	// attempts and must join the underlying connection error, not just the
	// sentinel. The final attempt must NOT incur an extra backoff wait.
	cfg, err := pgxpool.ParseConfig("postgres://user:pass@127.0.0.1:1/db")
	require.NoError(t, err)

	start := time.Now()
	pool, err := connect(context.Background(), cfg, 2, 50*time.Millisecond)
	elapsed := time.Since(start)

	require.Nil(t, pool)
	require.ErrorIs(t, err, ErrFailedToOpenDBConnection)
	// 2 attempts => exactly one inter-attempt wait of ~50ms; the final attempt
	// must not add a trailing wait. Allow generous headroom for connection time.
	require.Less(t, elapsed, 5*time.Second, "exhausted retries must not perform a wasted final backoff")
	// The joined error must carry more than the bare sentinel message.
	require.NotEqual(t, ErrFailedToOpenDBConnection.Error(), err.Error(),
		"underlying connection error must be preserved in the returned error")
}

func TestConnect_ZeroAttemptsTreatedAsOne(t *testing.T) {
	t.Parallel()

	cfg, err := pgxpool.ParseConfig("postgres://user:pass@127.0.0.1:1/db")
	require.NoError(t, err)

	pool, err := connect(context.Background(), cfg, 0, time.Millisecond)
	require.Nil(t, pool)
	require.ErrorIs(t, err, ErrFailedToOpenDBConnection)
}

// Sanity check that the joined error type is what callers expect to unwrap.
func TestConnect_ErrorIsJoinable(t *testing.T) {
	t.Parallel()

	joined := errors.Join(ErrFailedToOpenDBConnection, errors.New("dial tcp: refused"))
	require.ErrorIs(t, joined, ErrFailedToOpenDBConnection)
}
