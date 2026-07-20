//go:build integration

package pgworkflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/workflow"
	pgworkflow "github.com/dmitrymomot/forge/async/workflow/postgres"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/resilience/backoff"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ workflow.Store = (*pgworkflow.Store)(nil)

// openPool connects to the suite's Postgres (via pgtest.DSN) and applies the
// workflow_runs migration.
func openPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(tb)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(tb, err)
	tb.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	tb.Cleanup(func() { _ = db.Close() })
	require.NoError(tb, migration.New(pgworkflow.Migrations, migration.WithTable("forge_workflow_schema")).Up(context.Background(), db))
	_, err = pool.Exec(context.Background(), "TRUNCATE workflow_runs")
	require.NoError(tb, err)
	return pool
}

func testRun(id string) workflow.Run {
	now := time.Now().UTC().Truncate(time.Microsecond) // timestamptz keeps microseconds
	return workflow.Run{
		ID:        id,
		Workflow:  "wf.test",
		Scope:     "tenant-1",
		Status:    workflow.StatusRunning,
		State:     json.RawMessage(`{"n":1}`),
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("nil pool", func(t *testing.T) {
		t.Parallel()
		_, err := pgworkflow.New(nil)
		assert.Error(t, err)
	})

	t.Run("invalid table", func(t *testing.T) {
		t.Parallel()
		_, err := pgworkflow.New(&pgxpool.Pool{}, pgworkflow.WithTable("bad; DROP TABLE x"))
		assert.Error(t, err)
	})

	t.Run("table name over the identifier limit", func(t *testing.T) {
		t.Parallel()
		_, err := pgworkflow.New(&pgxpool.Pool{}, pgworkflow.WithTable(strings.Repeat("x", 64)))
		assert.Error(t, err)
	})
}

func TestStore_CreateGetUpdate(t *testing.T) {
	pool := openPool(t)
	store, err := pgworkflow.New(pool)
	require.NoError(t, err)
	ctx := context.Background()

	t.Run("create requires id", func(t *testing.T) {
		assert.Error(t, store.Create(ctx, workflow.Run{}))
	})

	t.Run("roundtrip", func(t *testing.T) {
		want := testRun("r-roundtrip")
		require.NoError(t, store.Create(ctx, want))

		got, err := store.Get(ctx, "r-roundtrip")
		require.NoError(t, err)
		assert.Equal(t, want.ID, got.ID)
		assert.Equal(t, want.Workflow, got.Workflow)
		assert.Equal(t, want.Scope, got.Scope)
		assert.Equal(t, want.Status, got.Status)
		assert.JSONEq(t, string(want.State), string(got.State))
		assert.Equal(t, want.Version, got.Version)
		assert.WithinDuration(t, want.CreatedAt, got.CreatedAt, time.Millisecond)
		assert.WithinDuration(t, want.UpdatedAt, got.UpdatedAt, time.Millisecond)
	})

	t.Run("create duplicate", func(t *testing.T) {
		require.NoError(t, store.Create(ctx, testRun("r-dup")))
		assert.ErrorIs(t, store.Create(ctx, testRun("r-dup")), workflow.ErrRunAlreadyExists)
	})

	t.Run("get missing", func(t *testing.T) {
		_, err := store.Get(ctx, "r-missing")
		assert.ErrorIs(t, err, workflow.ErrRunNotFound)
	})

	t.Run("update missing", func(t *testing.T) {
		assert.ErrorIs(t, store.Update(ctx, testRun("r-update-missing")), workflow.ErrRunNotFound)
	})

	t.Run("update bumps version and stale write is rejected", func(t *testing.T) {
		require.NoError(t, store.Create(ctx, testRun("r-version")))

		run, err := store.Get(ctx, "r-version")
		require.NoError(t, err)
		run.Step = 2
		run.Status = workflow.StatusCompensating
		run.Error = "boom"
		run.State = json.RawMessage(`{"n":2}`)
		require.NoError(t, store.Update(ctx, run))

		got, err := store.Get(ctx, "r-version")
		require.NoError(t, err)
		assert.Equal(t, 2, got.Version)
		assert.Equal(t, 2, got.Step)
		assert.Equal(t, workflow.StatusCompensating, got.Status)
		assert.Equal(t, "boom", got.Error)
		assert.JSONEq(t, `{"n":2}`, string(got.State))

		// The pre-update copy is stale now: its write must not regress the row.
		assert.ErrorIs(t, store.Update(ctx, run), workflow.ErrStaleRun)
	})
}

func TestPurgeTerminalBefore(t *testing.T) {
	pool := openPool(t)
	store, err := pgworkflow.New(pool)
	require.NoError(t, err)
	ctx := context.Background()
	cutoff := time.Now().UTC()

	old := func(id string, status workflow.Status) workflow.Run {
		run := testRun(id)
		run.Status = status
		run.CreatedAt = cutoff.Add(-time.Hour)
		run.UpdatedAt = cutoff.Add(-time.Hour)
		return run
	}
	require.NoError(t, store.Create(ctx, old("r-old-completed", workflow.StatusCompleted)))
	require.NoError(t, store.Create(ctx, old("r-old-failed", workflow.StatusFailed)))
	require.NoError(t, store.Create(ctx, old("r-old-running", workflow.StatusRunning)))
	require.NoError(t, store.Create(ctx, old("r-old-compensating", workflow.StatusCompensating)))
	fresh := testRun("r-fresh-completed")
	fresh.Status = workflow.StatusCompleted
	require.NoError(t, store.Create(ctx, fresh))

	t.Run("nil pool", func(t *testing.T) {
		_, err := pgworkflow.PurgeTerminalBefore(ctx, nil, cutoff)
		assert.Error(t, err)
	})

	n, err := pgworkflow.PurgeTerminalBefore(ctx, pool, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "only old terminal runs purge")

	for _, id := range []string{"r-old-running", "r-old-compensating", "r-fresh-completed"} {
		_, err := store.Get(ctx, id)
		assert.NoError(t, err, "run %s must survive the purge", id)
	}
	for _, id := range []string{"r-old-completed", "r-old-failed"} {
		_, err := store.Get(ctx, id)
		assert.ErrorIs(t, err, workflow.ErrRunNotFound, "run %s must be purged", id)
	}

	n, err = pgworkflow.PurgeTerminalBefore(ctx, pool, cutoff)
	require.NoError(t, err)
	assert.Zero(t, n)
}

// TestEngineOverPostgresStore drives a full saga — forward failure, reverse
// compensation — through the queue engine with checkpoints in Postgres.
func TestEngineOverPostgresStore(t *testing.T) {
	pool := openPool(t)
	store, err := pgworkflow.New(pool)
	require.NoError(t, err)
	ctx := context.Background()

	type payout struct {
		Undone bool `json:"undone"`
		Tries  int  `json:"tries"`
	}
	wf := workflow.New("wf.pg.saga",
		workflow.Step[payout]{
			Name:       "debit",
			Run:        func(context.Context, *payout) error { return nil },
			Compensate: func(_ context.Context, p *payout) error { p.Undone = true; return nil },
		},
		workflow.Step[payout]{
			Name:        "transfer",
			MaxAttempts: 2,
			Run:         func(context.Context, *payout) error { return errors.New("wire down") },
		},
	)
	eng := workflow.NewEngine(queue.NewMemoryBroker(), store)
	workflow.Register(eng, wf, workflow.WithRetryBackoff(backoff.Constant(0)))
	cfg := queue.DefaultConfig()
	cfg.PollInterval = 5 * time.Millisecond
	svc, err := workflow.NewService(eng, queue.WithConfig(cfg))
	require.NoError(t, err)
	svcCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() { _ = svc.Run(svcCtx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })

	runID, err := workflow.Start(ctx, eng, wf, payout{})
	require.NoError(t, err)

	var run workflow.Run
	require.Eventually(t, func() bool {
		run, err = store.Get(ctx, runID)
		require.NoError(t, err)
		return run.Status == workflow.StatusFailed
	}, 10*time.Second, 10*time.Millisecond)

	var p payout
	require.NoError(t, json.Unmarshal(run.State, &p))
	assert.True(t, p.Undone, "compensation must have run against the Postgres checkpoint")
	assert.Contains(t, run.Error, "wire down")
}
