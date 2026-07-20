//go:build integration

package pgeventbus_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/eventbus"
	pgeventbus "github.com/dmitrymomot/forge/async/eventbus/postgres"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ eventbus.Inbox = (*pgeventbus.Inbox)(nil)

// openPool connects to the suite's Postgres (via pgtest.DSN) and applies the
// inbox migration.
func openPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(tb)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(tb, err)
	tb.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	tb.Cleanup(func() { _ = db.Close() })
	require.NoError(tb, migration.New(pgeventbus.Migrations, migration.WithTable("forge_eventbus_schema")).Up(context.Background(), db))
	_, err = pool.Exec(context.Background(), "TRUNCATE eventbus_inbox")
	require.NoError(tb, err)
	return pool
}

func inTx(tb testing.TB, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tb.Helper()
	return pgx.BeginFunc(context.Background(), pool, fn)
}

func TestNewInbox(t *testing.T) {
	t.Parallel()

	t.Run("empty consumer", func(t *testing.T) {
		t.Parallel()
		_, err := pgeventbus.NewInbox("")
		assert.Error(t, err)
	})

	t.Run("invalid table", func(t *testing.T) {
		t.Parallel()
		_, err := pgeventbus.NewInbox("c", pgeventbus.WithTable("bad; DROP TABLE x"))
		assert.Error(t, err)
	})
}

func TestInbox_Seen(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()

	t.Run("first claim then duplicate", func(t *testing.T) {
		inbox, err := pgeventbus.NewInbox("sub.first")
		require.NoError(t, err)

		require.NoError(t, inTx(t, pool, func(tx pgx.Tx) error {
			seen, err := inbox.Seen(ctx, tx, "evt-1")
			require.NoError(t, err)
			assert.False(t, seen)
			return nil
		}))
		require.NoError(t, inTx(t, pool, func(tx pgx.Tx) error {
			seen, err := inbox.Seen(ctx, tx, "evt-1")
			require.NoError(t, err)
			assert.True(t, seen)
			return nil
		}))
	})

	t.Run("rollback releases the claim", func(t *testing.T) {
		inbox, err := pgeventbus.NewInbox("sub.rollback")
		require.NoError(t, err)

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		seen, err := inbox.Seen(ctx, tx, "evt-1")
		require.NoError(t, err)
		assert.False(t, seen)
		require.NoError(t, tx.Rollback(ctx))

		require.NoError(t, inTx(t, pool, func(tx pgx.Tx) error {
			seen, err := inbox.Seen(ctx, tx, "evt-1")
			require.NoError(t, err)
			assert.False(t, seen, "a rolled-back claim must not stick")
			return nil
		}))
	})

	t.Run("consumers dedup independently", func(t *testing.T) {
		a, err := pgeventbus.NewInbox("sub.a")
		require.NoError(t, err)
		b, err := pgeventbus.NewInbox("sub.b")
		require.NoError(t, err)

		require.NoError(t, inTx(t, pool, func(tx pgx.Tx) error {
			seen, err := a.Seen(ctx, tx, "evt-shared")
			require.NoError(t, err)
			assert.False(t, seen)
			return nil
		}))
		require.NoError(t, inTx(t, pool, func(tx pgx.Tx) error {
			seen, err := b.Seen(ctx, tx, "evt-shared")
			require.NoError(t, err)
			assert.False(t, seen, "another consumer's claim must not shadow this one")
			return nil
		}))
	})

	t.Run("rejects non-pgx tx and empty id", func(t *testing.T) {
		inbox, err := pgeventbus.NewInbox("sub.guard")
		require.NoError(t, err)
		_, err = inbox.Seen(ctx, "not a tx", "evt-1")
		assert.Error(t, err)
		require.NoError(t, inTx(t, pool, func(tx pgx.Tx) error {
			_, err := inbox.Seen(ctx, tx, "")
			assert.Error(t, err)
			return nil
		}))
	})
}

func TestPurgeSeenBefore(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()

	inbox, err := pgeventbus.NewInbox("sub.purge")
	require.NoError(t, err)
	require.NoError(t, inTx(t, pool, func(tx pgx.Tx) error {
		for _, id := range []string{"old-1", "old-2", "fresh-1"} {
			if _, err := inbox.Seen(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	}))
	_, err = pool.Exec(ctx, "UPDATE eventbus_inbox SET seen_at = now() - interval '48 hours' WHERE event_id LIKE 'old-%'")
	require.NoError(t, err)

	n, err := pgeventbus.PurgeSeenBefore(ctx, pool, time.Now().UTC().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	require.NoError(t, inTx(t, pool, func(tx pgx.Tx) error {
		seen, err := inbox.Seen(ctx, tx, "fresh-1")
		require.NoError(t, err)
		assert.True(t, seen, "fresh rows must survive the purge")
		seen, err = inbox.Seen(ctx, tx, "old-1")
		require.NoError(t, err)
		assert.False(t, seen, "purged rows are claimable again")
		return nil
	}))

	t.Run("nil pool", func(t *testing.T) {
		_, err := pgeventbus.PurgeSeenBefore(ctx, nil, time.Now())
		assert.Error(t, err)
	})
}
