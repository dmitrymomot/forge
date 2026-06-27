package redis_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	goredis "github.com/redis/go-redis/v9"

	forgeredis "github.com/dmitrymomot/forge/redis"
)

// validConfig is a tiny, fast Config used by tests that must pass Validate but never
// (or only briefly) touch the network.
func validConfig() forgeredis.Config {
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{"127.0.0.1:6379"}
	return cfg
}

// slogDiscard is the shared test logger. Defined here for reuse by later task tests
// (RD-3, RD-4); suppress unused until those files are added.
//
//nolint:unused
func slogDiscard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestBuildOptions(t *testing.T) {
	// buildOptions is unexported, so it is exercised indirectly: Open(...) feeds the
	// produced *goredis.UniversalOptions to goredis.NewUniversalClient, which we can
	// observe through WithUniversalOptions — the escape hatch runs LAST and receives
	// the fully-built options, letting us assert the Config -> UniversalOptions map.
	cfg := validConfig()
	cfg.Addresses = []string{"10.0.0.1:6379", "10.0.0.2:6379"}
	cfg.MasterName = "mymaster"
	cfg.Username = "user"
	cfg.Password = "secret"
	cfg.DB = 7
	cfg.PoolSize = 42
	cfg.MinIdleConns = 5
	cfg.ConnMaxIdleTime = 9 * time.Minute
	cfg.RetryAttempts = 1 // single attempt so Open returns fast after observing opts
	cfg.RetryInterval = time.Millisecond
	cfg.DialTimeout = 50 * time.Millisecond
	cfg.ReadTimeout = 50 * time.Millisecond
	cfg.WriteTimeout = 50 * time.Millisecond

	var seen *goredis.UniversalOptions
	// The dial will fail (nothing listening), but the escape hatch fires BEFORE the
	// dial, capturing the mapped options regardless of connectivity.
	_, _ = forgeredis.Open(t.Context(),
		forgeredis.WithConfig(cfg),
		forgeredis.WithUniversalOptions(func(o *goredis.UniversalOptions) {
			seen = o
		}),
	)
	require.NotNil(t, seen, "the escape hatch must run with the built options")
	assert.Equal(t, cfg.Addresses, seen.Addrs)
	assert.Equal(t, "mymaster", seen.MasterName)
	assert.Equal(t, "user", seen.Username)
	assert.Equal(t, "secret", seen.Password)
	assert.Equal(t, 7, seen.DB)
	assert.Equal(t, 42, seen.PoolSize)
	assert.Equal(t, 5, seen.MinIdleConns)
	assert.Equal(t, 9*time.Minute, seen.ConnMaxIdleTime)
	assert.Equal(t, 50*time.Millisecond, seen.DialTimeout)
	assert.Equal(t, 50*time.Millisecond, seen.ReadTimeout)
	assert.Equal(t, 50*time.Millisecond, seen.WriteTimeout)
	// Topology note: goredis.NewUniversalClient selects standalone/cluster/sentinel
	// from Addrs + MasterName above — here MasterName is set, so it builds a
	// failover (sentinel) client. The mapping is what we assert; the selection is the
	// driver's documented behavior.
}

func TestOpen_RetryExhausted(t *testing.T) {
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{"127.0.0.1:1"} // port 1: nothing listens, refused fast
	cfg.RetryAttempts = 2
	cfg.RetryInterval = time.Millisecond
	cfg.DialTimeout = 50 * time.Millisecond
	cfg.ReadTimeout = 50 * time.Millisecond
	cfg.WriteTimeout = 50 * time.Millisecond

	start := time.Now()
	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeredis.ErrConnect, "exhausted retries report ErrConnect")
	assert.Nil(t, c, "a failed Open returns no client and leaks none")
	// 2 attempts with a 1ms base backoff must not hang; generous ceiling for CI.
	assert.Less(t, time.Since(start), 10*time.Second)
}

func TestOpen_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // already cancelled: Open must honor it and not spin the full backoff

	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{"127.0.0.1:1"}
	cfg.RetryAttempts = 50
	cfg.RetryInterval = time.Second // huge base; if ctx were ignored this would hang
	cfg.DialTimeout = 50 * time.Millisecond

	start := time.Now()
	c, err := forgeredis.Open(ctx, forgeredis.WithConfig(cfg))
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Less(t, time.Since(start), 5*time.Second, "Open must abort promptly on a cancelled ctx")
}
