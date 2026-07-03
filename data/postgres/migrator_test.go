package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/postgres"
)

// fakeMigrator records whether Up was called and returns a fixed error.
type fakeMigrator struct {
	called bool
	err    error
}

func (f *fakeMigrator) Up(_ context.Context, db *sql.DB) error {
	f.called = true
	return f.err
}

func TestWithMigrator_NilRejected(t *testing.T) {
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	pool, err := postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithMigrator(nil),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrInvalidConfig)
	assert.Nil(t, pool)
}

func TestWithMigrator_NotRunWhenConnectFails(t *testing.T) {
	// Unreachable addr: Open never reaches a live pool, so the migrator must not run.
	cfg := postgres.DefaultConfig()
	cfg.URL = "postgres://u:p@127.0.0.1:1/db"
	cfg.RetryAttempts = 1
	cfg.ConnectTimeout = 100 * time.Millisecond
	fm := &fakeMigrator{}
	pool, err := postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithMigrator(fm),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, postgres.ErrConnect)
	assert.Nil(t, pool)
	assert.False(t, fm.called, "migrator must not run when the pool never came up")
}

func TestWithMigrator_RunsAfterConnect_Integration(t *testing.T) {
	dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FORGE_TEST_POSTGRES_DSN")
	}
	cfg := postgres.DefaultConfig()
	cfg.URL = dsn

	// Success path: the migrator runs and Open returns a live pool.
	ok := &fakeMigrator{}
	pool, err := postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithMigrator(ok),
	)
	require.NoError(t, err)
	require.NotNil(t, pool)
	assert.True(t, ok.called, "migrator must run after a live+pinged pool")
	postgres.Close(pool, nil)

	// Failure path: a migrator error fails Open and leaks no pool.
	boom := errors.New("migration boom")
	fail := &fakeMigrator{err: boom}
	pool, err = postgres.Open(context.Background(),
		postgres.WithConfig(cfg),
		postgres.WithMigrator(fail),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, boom, "a failed migration must surface as a failed Open")
	assert.Nil(t, pool)
	assert.True(t, fail.called)
}
