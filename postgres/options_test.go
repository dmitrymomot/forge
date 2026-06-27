package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/postgres"
)

func TestOpen_NilOptionRejected(t *testing.T) {
	opts := map[string]postgres.Option{
		"logger":     postgres.WithLogger(nil),
		"poolconfig": postgres.WithPoolConfig(nil),
	}
	for name, opt := range opts {
		t.Run(name, func(t *testing.T) {
			// A valid URL is supplied so the only failure is the option's rejection.
			cfg := postgres.DefaultConfig()
			cfg.URL = "postgres://u:p@127.0.0.1:1/db"
			pool, err := postgres.Open(context.Background(),
				postgres.WithConfig(cfg),
				opt,
			)
			require.Error(t, err)
			assert.ErrorIs(t, err, postgres.ErrInvalidConfig)
			assert.Nil(t, pool)
		})
	}
}

func TestOpen_MissingURL(t *testing.T) {
	// No WithConfig => pure DefaultConfig => empty URL => Validate fails.
	pool, err := postgres.Open(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrInvalidConfig)
	assert.Nil(t, pool)
}

func TestOpen_BadURL(t *testing.T) {
	// Non-empty but unparseable URL passes Validate, then fails ParseConfig.
	cfg := postgres.DefaultConfig()
	cfg.URL = "://not a url"
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrConnect)
	assert.Nil(t, pool)
}

func TestWithPoolConfig_RunsLast(t *testing.T) {
	// The escape hatch runs after the Config overlay, so it sees Config's MaxConns
	// and can override it. We assert it is invoked and observes the overlaid value.
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	cfg.MaxConns = 7
	cfg.RetryAttempts = 1
	cfg.RetryInterval = time.Millisecond
	cfg.ConnectTimeout = 100 * time.Millisecond

	var sawMaxConns int32
	called := false
	// Open will ultimately fail to connect (unreachable addr), but the hatch runs
	// before the connect attempt, so we can observe it ran with the overlay applied.
	_, _ = postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithPoolConfig(func(pc *pgxpool.Config) {
			called = true
			sawMaxConns = pc.MaxConns
		}),
	)
	require.True(t, called, "WithPoolConfig fn must run inside Open")
	assert.Equal(t, int32(7), sawMaxConns, "hatch runs after the Config overlay")
}
