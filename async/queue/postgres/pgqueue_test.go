//go:build integration

package pgqueue_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/queue/brokertest"
	pgqueue "github.com/dmitrymomot/forge/async/queue/postgres"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var (
	_ queue.Broker   = (*pgqueue.Broker)(nil)
	_ queue.TxPusher = (*pgqueue.Broker)(nil)
)

// openPool connects to the suite's Postgres (via pgtest.DSN) and applies the
// queue migration.
func openPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(tb)
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
	_, err := pool.Exec(context.Background(), "TRUNCATE queue_jobs, queue_jobs_dead")
	require.NoError(tb, err)
	b, err := pgqueue.New(pool)
	require.NoError(tb, err)
	return b
}

// claimOne polls Claim until the queue yields a job or a deadline passes. Jobs
// pushed via the client carry a RunAt stamped from the test-process clock, but
// visibility is decided by the Postgres clock; polling tolerates a containerised
// database whose clock lags the test process (e.g. a Docker VM under load).
func claimOne(t *testing.T, b queue.Broker, q string) queue.ClaimedJob {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := b.Claim(ctx, q, 10, time.Minute)
		require.NoError(t, err)
		if len(got) >= 1 {
			return got[0]
		}
		if time.Now().After(deadline) {
			require.Len(t, got, 1, "expected 1 claimable job within deadline")
			return queue.ClaimedJob{}
		}
		time.Sleep(25 * time.Millisecond)
	}
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
		job := claimOne(t, b, "default")
		require.NoError(t, b.Ack(ctx, job.ID, job.Token))
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
	// The nil-pool rejection is a pure-unit check in validate_test.go; this one
	// needs a real pool to reach the table-name guard.
	pool := openPool(t)
	_, err := pgqueue.New(pool, pgqueue.WithTable("bad;name"))
	require.Error(t, err, "unsafe table name rejected")
}

func TestPgQueue_PayloadRoundTripsJSON(t *testing.T) {
	pool := openPool(t)
	b := newBroker(t, pool)
	ctx := context.Background()
	c := queue.NewClient(b)
	require.NoError(t, c.PushRaw(ctx, "raw.kind", json.RawMessage(`{"deep":{"x":[1,2,3]}}`)))
	job := claimOne(t, b, "default")
	assert.JSONEq(t, `{"deep":{"x":[1,2,3]}}`, string(job.Payload))
}

func TestPgQueue_StatsCapped(t *testing.T) {
	pool := openPool(t)
	b := newBroker(t, pool)
	ctx := context.Background()

	jobs := make([]queue.Job, 10001)
	for i := range jobs {
		jobs[i] = queue.Job{
			ID: id.NewUUID().String(), Queue: "bulk", Type: "cap.kind",
			Payload: []byte(`{}`), RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
		}
	}
	require.NoError(t, b.Push(ctx, jobs...))

	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 10000, st["bulk"].Pending, "count reports the cap, not the true size")
	assert.True(t, st["bulk"].PendingCapped)
	assert.False(t, st["bulk"].DeadCapped)
}

// TestPgQueue_PurgeDeadBeforeSpansManyBatches covers the batched drain loop
// that brokertest's PurgeDeadBefore subtest cannot reach: it purges a single
// dead job, so one statement always suffices and an off-by-one in the loop — a
// batch silently left behind, or a miscounted total — would go unnoticed.
// 12001 dead rows span two full purgeBatch(5000) statements plus a short one,
// and the odd row proves the loop's exit does not depend on an exact multiple.
//
// The rows are inserted straight into the dead table rather than pushed,
// claimed and killed one by one: the sweep reads nothing but died_at, and
// 12001 round-trip Kill calls would dominate the runtime without testing
// anything this test is about. Cutoffs sit far either side of the fixture's
// died_at values, so a lagging container clock cannot flip the outcome.
func TestPgQueue_PurgeDeadBeforeSpansManyBatches(t *testing.T) {
	pool := openPool(t)
	b := newBroker(t, pool)
	ctx := context.Background()

	const dead = 12001 // > 2x the 5000 batch, deliberately not a multiple of it
	_, err := pool.Exec(ctx, `INSERT INTO queue_jobs_dead
(id, queue, type, payload, scope, attempt, max_attempts, run_at, created_at, died_at, last_error)
SELECT gen_random_uuid(), 'purgebatch', 'purge.kind', '{}'::json, '', 1, 5,
       now() - interval '40 days', now() - interval '40 days', now() - interval '40 days', 'retention fixture'
FROM generate_series(1, $1)`, dead)
	require.NoError(t, err)

	countDead := func() int {
		var n int
		require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM queue_jobs_dead").Scan(&n))
		return n
	}
	// Premise guard: without this, an assertion of "everything is gone" could
	// pass on a dead table that was never populated.
	require.Equal(t, dead, countDead(), "fixture must fill the dead table")

	// A cutoff before every died_at removes nothing: the loop must exit on the
	// first empty batch rather than spin.
	n, err := b.PurgeDeadBefore(ctx, time.Now().Add(-365*24*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
	require.Equal(t, dead, countDead(), "a cutoff older than every row must purge nothing")

	n, err = b.PurgeDeadBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, dead, n, "the batched loop must report every purged row exactly once across all batches")
	assert.Zero(t, countDead(), "no dead row may survive the sweep")

	left, err := b.ListDead(ctx, "purgebatch", 10)
	require.NoError(t, err)
	assert.Empty(t, left)
}

// TestPgQueue_PushCopyFromThreshold exercises Push/PushTx right at
// copyFromThreshold, where the insert switches from the unnest-array INSERT
// to pgx.CopyFrom (see pgqueue.go). TestPgQueue_StatsCapped already pushes a
// larger batch through this path but only checks the row count.
//
// Every row here carries distinct, per-row-derived values in every column
// that could plausibly transpose (type, scope, payload, attempt,
// max_attempts, run_at, created_at): identical rows can't catch a column
// mis-mapping in pushCopyFrom/copyFromCols, since swapping two columns of
// identical values is invisible. Queue is deliberately left constant per
// subtest instead — it doubles as the Claim filter key, so a queue<->other
// transposition still surfaces loudly (wrong claimed-row-count below) without
// needing its own per-row uniqueness.
//
// runAt is strictly increasing by row index and Claim returns jobs ordered by
// (run_at, id), so the claimed slice comes back in build order and each
// sampled index can be checked directly against what newJobs built for it,
// with no id-keyed lookup required.
func TestPgQueue_PushCopyFromThreshold(t *testing.T) {
	pool := openPool(t)
	const n = 2000 // matches copyFromThreshold

	// newJobs builds n rows for queue q. runAt starts 5s in the past (past-
	// biased: the DB clock can lag the test process on a Docker VM) and
	// climbs 1ms per row, so even the last row (run_at = now-5s+1999ms =
	// now-3.001s) is comfortably due by the time Claim runs.
	newJobs := func(q string) []queue.Job {
		runAtBase := time.Now().UTC().Add(-5 * time.Second).Truncate(time.Microsecond)
		createdBase := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
		jobs := make([]queue.Job, n)
		for i := range jobs {
			jobs[i] = queue.Job{
				ID:          id.NewUUID().String(),
				Queue:       q,
				Type:        fmt.Sprintf("copy.kind.%d", i),
				Payload:     []byte(fmt.Sprintf(`{"n":%d}`, i)),
				Scope:       fmt.Sprintf("tenant-%d", i),
				Attempt:     i,
				MaxAttempts: i + 10000, // offset well clear of attempt (and attempt+1 post-claim) so a swap can't hide behind equal values
				RunAt:       runAtBase.Add(time.Duration(i) * time.Millisecond),
				CreatedAt:   createdBase.Add(time.Duration(i) * time.Second),
			}
		}
		return jobs
	}

	// assertRow checks the claimed job at build-index i against the exact
	// values newJobs assigned it — the proof that column i landed back in
	// field i, not some neighbor's. Claim increments Attempt by one over the
	// pushed value (see claimSQL), hence i+1.
	assertRow := func(t *testing.T, q string, i int, got queue.ClaimedJob, runAtBase, createdBase time.Time) {
		t.Helper()
		assert.Equal(t, q, got.Queue, "row %d: queue", i)
		assert.Equal(t, fmt.Sprintf("copy.kind.%d", i), got.Type, "row %d: type", i)
		assert.JSONEq(t, fmt.Sprintf(`{"n":%d}`, i), string(got.Payload), "row %d: payload", i)
		assert.Equal(t, fmt.Sprintf("tenant-%d", i), got.Scope, "row %d: scope", i)
		assert.Equal(t, i+1, got.Attempt, "row %d: attempt (post-claim)", i)
		assert.Equal(t, i+10000, got.MaxAttempts, "row %d: max_attempts", i)
		assert.True(t, runAtBase.Add(time.Duration(i)*time.Millisecond).Equal(got.RunAt), "row %d: run_at", i)
		assert.True(t, createdBase.Add(time.Duration(i)*time.Second).Equal(got.CreatedAt), "row %d: created_at", i)
	}

	// sampleIndexes are spread across the batch (first, several interior, last)
	// so a transposition affecting only some rows (unlikely, but cheap to
	// guard) still has a good chance of being sampled.
	sampleIndexes := []int{0, 1, 500, 999, 1500, 1999}

	t.Run("Push", func(t *testing.T) {
		b := newBroker(t, pool)
		ctx := context.Background()
		jobs := newJobs("copyfrom-push")
		require.NoError(t, b.Push(ctx, jobs...))

		got, err := b.Claim(ctx, "copyfrom-push", n, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, n, "all CopyFrom-inserted jobs must be claimable")
		for _, i := range sampleIndexes {
			assertRow(t, "copyfrom-push", i, got[i], jobs[0].RunAt, jobs[0].CreatedAt)
		}
	})

	t.Run("PushTx", func(t *testing.T) {
		b := newBroker(t, pool)
		ctx := context.Background()

		jobs := newJobs("copyfrom-tx")
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, b.PushTx(ctx, tx, jobs...))
		require.NoError(t, tx.Commit(ctx))

		got, err := b.Claim(ctx, "copyfrom-tx", n, time.Minute)
		require.NoError(t, err)
		require.Len(t, got, n, "all CopyFrom-inserted (tx) jobs must be claimable")
		for _, i := range sampleIndexes {
			assertRow(t, "copyfrom-tx", i, got[i], jobs[0].RunAt, jobs[0].CreatedAt)
		}
	})

	// PushTx_Rollback pins the "commits/rolls back with the caller's
	// transaction" contract specifically for the CopyFrom path: everything
	// tested above goes through commit, so without this the rollback branch
	// of the new write path would be unguarded.
	t.Run("PushTx_Rollback", func(t *testing.T) {
		b := newBroker(t, pool)
		ctx := context.Background()

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, b.PushTx(ctx, tx, newJobs("copyfrom-tx-rollback")...))
		require.NoError(t, tx.Rollback(ctx))

		got, err := b.Claim(ctx, "copyfrom-tx-rollback", n, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, got, "CopyFrom rows inside a rolled-back tx must leave nothing behind")
	})
}
