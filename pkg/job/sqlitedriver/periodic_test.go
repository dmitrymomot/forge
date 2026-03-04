package sqlitedriver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/job"

	_ "modernc.org/sqlite"
)

func TestPeriodicScheduler_InsertsJob(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(50*time.Millisecond))
	ctx := context.Background()

	cfg := job.WorkerConfig{
		Executor: func(_ context.Context, _ string, _ json.RawMessage) error {
			return nil
		},
		Queues: map[string]int{"default": 1},
		PeriodicJobs: []job.PeriodicJobConfig{
			{TaskName: "periodic_task", Schedule: "@every 1s"},
		},
	}

	require.NoError(t, d.Start(ctx, cfg))
	defer func() { _ = d.Stop(context.Background()) }()

	// Wait for the periodic scheduler to insert at least one job.
	require.Eventually(t, func() bool {
		var count int
		_ = db.QueryRow(`SELECT COUNT(*) FROM forge_jobs WHERE task_name = 'periodic_task'`).Scan(&count)
		return count > 0
	}, 3*time.Second, 50*time.Millisecond)

	// Verify the inserted job has the dedup unique_key.
	var uniqueKey string
	require.NoError(t, db.QueryRow(
		`SELECT unique_key FROM forge_jobs WHERE task_name = 'periodic_task' LIMIT 1`,
	).Scan(&uniqueKey))
	require.Equal(t, "periodic:periodic_task", uniqueKey)
}

func TestPeriodicScheduler_InvalidCron(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	cfg := job.WorkerConfig{
		Executor: func(_ context.Context, _ string, _ json.RawMessage) error { return nil },
		Queues:   map[string]int{"default": 1},
		PeriodicJobs: []job.PeriodicJobConfig{
			{TaskName: "bad", Schedule: "invalid cron"},
		},
	}

	err := d.Start(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cron schedule")
}

func TestPeriodicScheduler_StopsCleanly(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(50*time.Millisecond))
	ctx := context.Background()

	cfg := job.WorkerConfig{
		Executor: func(_ context.Context, _ string, _ json.RawMessage) error { return nil },
		Queues:   map[string]int{"default": 1},
		PeriodicJobs: []job.PeriodicJobConfig{
			{TaskName: "stop_test", Schedule: "@every 1s"},
		},
	}

	require.NoError(t, d.Start(ctx, cfg))

	// Stop should complete without hanging.
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, d.Stop(stopCtx))

	// Should be able to start again.
	require.NoError(t, d.Start(ctx, cfg))
	require.NoError(t, d.Stop(context.Background()))
}
