package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/postgres"
)

// fakeMigrator records whether Up was called and returns a fixed error. It is
// shared with the integration tier (migrator_integration_test.go).
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
