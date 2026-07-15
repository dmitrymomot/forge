package pgqueue_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/queue/brokertest"
	pgqueue "github.com/dmitrymomot/forge/async/queue/postgres"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
)

var (
	_ queue.Broker   = (*pgqueue.Broker)(nil)
	_ queue.TxPusher = (*pgqueue.Broker)(nil)
)

// openPool connects to the suite's Postgres (embedded by default; see TestMain)
// and applies the queue migration.
func openPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = testDSN
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(tb, err)
	tb.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	tb.Cleanup(func() { _ = db.Close() })
	require.NoError(tb, migration.New(pgqueue.Migrations, migration.WithTable("forge_queue_schema")).Up(context.Background(), db))
	return pool
}

func newBroker(tb testing.TB, pool *pgxpool.Pool) *pgqueue.Broker {
	tb.Helper()
	_, err := pool.Exec(context.Background(), "TRUNCATE queue_jobs")
	require.NoError(tb, err)
	b, err := pgqueue.New(pool)
	require.NoError(tb, err)
	return b
}

func TestPgQueue_Conformance(t *testing.T) {
	pool := openPool(t)
	brokertest.Run(t, func(t *testing.T) queue.Broker { return newBroker(t, pool) })
}

func TestPgQueue_PushTx(t *testing.T) {
	pool := openPool(t)
	b := newBroker(t, pool)
	ctx := context.Background()
	c := queue.NewClient(b)
	kind := queue.NewKind[struct {
		N int `json:"n"`
	}]("tx.kind")

	t.Run("commit makes the job claimable", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, queue.PushTx(ctx, c, tx, kind, struct {
			N int `json:"n"`
		}{N: 1}))

		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, got, "job must be invisible before commit")

		require.NoError(t, tx.Commit(ctx))
		got, err = b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.NoError(t, b.Ack(ctx, got[0].ID))
	})

	t.Run("rollback discards the job", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, queue.PushTx(ctx, c, tx, kind, struct {
			N int `json:"n"`
		}{N: 2}))
		require.NoError(t, tx.Rollback(ctx))

		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("wrong tx type errors", func(t *testing.T) {
		err := queue.PushTx(ctx, c, "not a tx", kind, struct {
			N int `json:"n"`
		}{N: 3})
		require.Error(t, err)
	})
}

func TestPgQueue_WithTableValidation(t *testing.T) {
	t.Parallel()
	_, err := pgqueue.New(nil)
	require.Error(t, err, "nil pool rejected")
	pool := openPool(t) // skips without env
	_, err = pgqueue.New(pool, pgqueue.WithTable("bad;name"))
	require.Error(t, err, "unsafe table name rejected")
}

func TestPgQueue_PayloadIsJSONB(t *testing.T) {
	pool := openPool(t)
	b := newBroker(t, pool)
	ctx := context.Background()
	c := queue.NewClient(b)
	require.NoError(t, c.PushRaw(ctx, "raw.kind", json.RawMessage(`{"deep":{"x":[1,2,3]}}`)))
	got, err := b.Claim(ctx, "default", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.JSONEq(t, `{"deep":{"x":[1,2,3]}}`, string(got[0].Payload))
}
