//go:build integration

package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/resilience/lock"
	"github.com/dmitrymomot/forge/resilience/lock/pgstore"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ lock.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_lock_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

func TestPgLock_AcquireExclusiveFenceRefreshRelease(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "k-" + t.Name()
	require.NoError(t, s.Release(ctx, key, "a"))
	require.NoError(t, s.Release(ctx, key, "b"))

	f1, ok, err := s.Acquire(ctx, key, "a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.NotZero(t, f1)

	_, ok, err = s.Acquire(ctx, key, "b", time.Minute) // held by a
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = s.Refresh(ctx, key, "a", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, s.Release(ctx, key, "a"))
	f2, ok, err := s.Acquire(ctx, key, "b", time.Minute) // now free
	require.NoError(t, err)
	require.True(t, ok)
	assert.Greater(t, f2, f1) // fence monotonic
	require.NoError(t, s.Release(ctx, key, "b"))
}

func TestPgLock_ExpiredLeaseIsReclaimable(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "exp-" + t.Name()
	require.NoError(t, s.Release(ctx, key, "a"))

	_, ok, err := s.Acquire(ctx, key, "a", 200*time.Millisecond)
	require.NoError(t, err)
	require.True(t, ok)
	time.Sleep(400 * time.Millisecond)
	_, ok, err = s.Acquire(ctx, key, "b", time.Minute) // a's lease expired
	require.NoError(t, err)
	assert.True(t, ok)
	require.NoError(t, s.Release(ctx, key, "b"))
}
