//go:build integration

package pgscheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/scheduler"
	pgscheduler "github.com/dmitrymomot/forge/async/scheduler/postgres"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ scheduler.Store = (*pgscheduler.Store)(nil)

// openPool connects to the suite's Postgres (via pgtest.DSN) and applies the
// claims migration.
func openPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(tb)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(tb, err)
	tb.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	tb.Cleanup(func() { _ = db.Close() })
	require.NoError(tb, migration.New(pgscheduler.Migrations, migration.WithTable("forge_scheduler_schema")).Up(context.Background(), db))
	_, err = pool.Exec(context.Background(), "TRUNCATE scheduler_claims")
	require.NoError(tb, err)
	return pool
}

func TestNewStore(t *testing.T) {
	t.Run("nil pool", func(t *testing.T) {
		_, err := pgscheduler.NewStore(nil)
		assert.Error(t, err)
	})

	t.Run("invalid table", func(t *testing.T) {
		pool := openPool(t)
		_, err := pgscheduler.NewStore(pool, pgscheduler.WithTable("bad; DROP TABLE x"))
		assert.Error(t, err)
	})

	t.Run("overlong table", func(t *testing.T) {
		pool := openPool(t)
		_, err := pgscheduler.NewStore(pool, pgscheduler.WithTable(strings64()))
		assert.Error(t, err)
	})
}

func strings64() string {
	s := make([]byte, 64)
	for i := range s {
		s[i] = 'a'
	}
	return string(s)
}

func TestClaim(t *testing.T) {
	pool := openPool(t)
	store, err := pgscheduler.NewStore(pool)
	require.NoError(t, err)
	ctx := context.Background()
	tick := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

	t.Run("claim once per tick", func(t *testing.T) {
		require.NoError(t, store.Claim(ctx, "job.a", tick))
		require.ErrorIs(t, store.Claim(ctx, "job.a", tick), scheduler.ErrAlreadyClaimed)
		// Other names and ticks are independent claims.
		require.NoError(t, store.Claim(ctx, "job.b", tick))
		require.NoError(t, store.Claim(ctx, "job.a", tick.Add(time.Minute)))
	})

	t.Run("keyed by instant not location", func(t *testing.T) {
		require.NoError(t, store.Claim(ctx, "job.tz", tick))
		require.ErrorIs(t, store.Claim(ctx, "job.tz", tick.In(time.FixedZone("plus2", 7200))), scheduler.ErrAlreadyClaimed)
	})

	t.Run("empty name rejected", func(t *testing.T) {
		require.Error(t, store.Claim(ctx, "", tick))
	})
}

func TestClaimRace(t *testing.T) {
	pool := openPool(t)
	store, err := pgscheduler.NewStore(pool)
	require.NoError(t, err)
	ctx := context.Background()
	tick := time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)

	const racers = 16
	var wg sync.WaitGroup
	wins := make(chan struct{}, racers)
	for range racers {
		wg.Go(func() {
			if store.Claim(ctx, "job.race", tick) == nil {
				wins <- struct{}{}
			}
		})
	}
	wg.Wait()
	assert.Len(t, wins, 1)
}

func TestRelease(t *testing.T) {
	pool := openPool(t)
	store, err := pgscheduler.NewStore(pool)
	require.NoError(t, err)
	ctx := context.Background()
	tick := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	require.NoError(t, store.Claim(ctx, "job.rel", tick))
	require.NoError(t, store.Release(ctx, "job.rel", tick))
	require.NoError(t, store.Claim(ctx, "job.rel", tick), "released tick must be claimable again")
	// Releasing an absent claim is a no-op.
	require.NoError(t, store.Release(ctx, "job.ghost", tick))
}

func TestPurgeBefore(t *testing.T) {
	pool := openPool(t)
	store, err := pgscheduler.NewStore(pool)
	require.NoError(t, err)
	ctx := context.Background()
	base := time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)

	require.NoError(t, store.Claim(ctx, "job.p", base.Add(-2*time.Hour)))
	require.NoError(t, store.Claim(ctx, "job.p", base.Add(-90*time.Minute)))
	require.NoError(t, store.Claim(ctx, "job.p", base.Add(-30*time.Minute)))

	n, err := store.PurgeBefore(ctx, base.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Purged ticks are claimable again; the kept one is not.
	require.NoError(t, store.Claim(ctx, "job.p", base.Add(-2*time.Hour)))
	require.ErrorIs(t, store.Claim(ctx, "job.p", base.Add(-30*time.Minute)), scheduler.ErrAlreadyClaimed)

	n, err = store.PurgeBefore(ctx, base.Add(-3*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
}
