//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

func TestWithMigrator_RunsAfterConnect_Integration(t *testing.T) {
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)

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
