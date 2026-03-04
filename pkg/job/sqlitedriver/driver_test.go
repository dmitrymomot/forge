package sqlitedriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/job"

	_ "modernc.org/sqlite"
)

// testDB creates an in-memory SQLite database with the forge_jobs table.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	// Force single connection so all goroutines share the same in-memory database.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	d := New(db)
	require.NoError(t, d.Migrate(context.Background()))
	return db
}

func TestNew_Defaults(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	d := New(db)
	require.Equal(t, defaultPollInterval, d.pollInterval)
	require.NotNil(t, d.logger)
	require.False(t, d.started)
}

func TestNew_WithOptions(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(db,
		WithLogger(logger),
		WithPollInterval(500*time.Millisecond),
	)
	require.Equal(t, 500*time.Millisecond, d.pollInterval)
}

func TestMigrate_CreatesTable(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	d := New(db)
	require.NoError(t, d.Migrate(context.Background()))

	// Verify table exists by inserting a row.
	_, err = db.Exec(
		`INSERT INTO forge_jobs (task_name) VALUES ('test_task')`,
	)
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM forge_jobs`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestMigrate_Idempotent(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	d := New(db)
	require.NoError(t, d.Migrate(context.Background()))
	require.NoError(t, d.Migrate(context.Background()))

	// Verify table still works.
	_, err = db.Exec(`INSERT INTO forge_jobs (task_name) VALUES ('test')`)
	require.NoError(t, err)
}

func TestInsert_BasicJob(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	ji := &job.JobInsert{
		TaskName:    "send_email",
		Queue:       "emails",
		Payload:     json.RawMessage(`{"to":"user@test.com"}`),
		Tags:        []string{"urgent", "transactional"},
		Priority:    2,
		MaxAttempts: 5,
	}
	require.NoError(t, d.Insert(context.Background(), ji))

	var (
		id          int64
		queue       string
		taskName    string
		payload     sql.NullString
		uniqueKey   string
		tags        string
		priority    int
		maxAttempts int
		attempt     int
		status      string
	)
	err := db.QueryRow(`SELECT id, queue, task_name, payload, unique_key, tags, priority, max_attempts, attempt, status FROM forge_jobs`).
		Scan(&id, &queue, &taskName, &payload, &uniqueKey, &tags, &priority, &maxAttempts, &attempt, &status)
	require.NoError(t, err)

	require.Equal(t, int64(1), id)
	require.Equal(t, "emails", queue)
	require.Equal(t, "send_email", taskName)
	require.True(t, payload.Valid)
	require.JSONEq(t, `{"to":"user@test.com"}`, payload.String)
	require.Equal(t, "", uniqueKey)
	require.JSONEq(t, `["urgent","transactional"]`, tags)
	require.Equal(t, 2, priority)
	require.Equal(t, 5, maxAttempts)
	require.Equal(t, 0, attempt)
	require.Equal(t, "pending", status)
}

func TestInsert_DefaultQueue(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	ji := &job.JobInsert{TaskName: "test_task"}
	require.NoError(t, d.Insert(context.Background(), ji))

	var queue string
	require.NoError(t, db.QueryRow(`SELECT queue FROM forge_jobs`).Scan(&queue))
	require.Equal(t, "default", queue)
}

func TestInsert_DefaultMaxAttempts(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	ji := &job.JobInsert{TaskName: "test_task"}
	require.NoError(t, d.Insert(context.Background(), ji))

	var maxAttempts int
	require.NoError(t, db.QueryRow(`SELECT max_attempts FROM forge_jobs`).Scan(&maxAttempts))
	require.Equal(t, 25, maxAttempts)
}

func TestInsert_EmptyPayload(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	ji := &job.JobInsert{TaskName: "test_task"}
	require.NoError(t, d.Insert(context.Background(), ji))

	var payload sql.NullString
	require.NoError(t, db.QueryRow(`SELECT payload FROM forge_jobs`).Scan(&payload))
	require.False(t, payload.Valid) // NULL
}

func TestInsert_ScheduledAt(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	future := time.Now().UTC().Add(1 * time.Hour)
	ji := &job.JobInsert{
		TaskName:    "test_task",
		ScheduledAt: &future,
	}
	require.NoError(t, d.Insert(context.Background(), ji))

	var scheduledAt string
	require.NoError(t, db.QueryRow(`SELECT scheduled_at FROM forge_jobs`).Scan(&scheduledAt))

	parsed, err := time.Parse(time.RFC3339Nano, scheduledAt)
	require.NoError(t, err)
	require.WithinDuration(t, future, parsed, time.Second)
}

func TestInsert_UniqueFor_SkipsDuplicate(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)
	ctx := context.Background()

	ji := &job.JobInsert{
		TaskName:  "unique_task",
		UniqueKey: "user:123",
		UniqueFor: 5 * time.Minute,
	}

	// First insert succeeds.
	require.NoError(t, d.Insert(ctx, ji))

	// Second insert is silently skipped (dedup).
	require.NoError(t, d.Insert(ctx, ji))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM forge_jobs`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestInsert_UniqueFor_AllowsAfterTerminalState(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)
	ctx := context.Background()

	ji := &job.JobInsert{
		TaskName:  "unique_task",
		UniqueKey: "user:456",
		UniqueFor: 5 * time.Minute,
	}

	require.NoError(t, d.Insert(ctx, ji))

	// Mark as completed (terminal state).
	_, err := db.Exec(`UPDATE forge_jobs SET status = 'completed'`)
	require.NoError(t, err)

	// New insert should succeed since the existing job is terminal.
	require.NoError(t, d.Insert(ctx, ji))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM forge_jobs`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestInsert_UniqueKeyWithoutUniqueFor(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)
	ctx := context.Background()

	ji := &job.JobInsert{
		TaskName:  "no_dedup_task",
		UniqueKey: "key:1",
	}

	// Both inserts should succeed (no dedup without UniqueFor).
	require.NoError(t, d.Insert(ctx, ji))
	require.NoError(t, d.Insert(ctx, ji))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM forge_jobs`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestInsertTx_Success(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)
	ctx := context.Background()

	tx, err := db.Begin()
	require.NoError(t, err)

	ji := &job.JobInsert{TaskName: "tx_task", Queue: "txq"}
	require.NoError(t, d.InsertTx(ctx, tx, ji))
	require.NoError(t, tx.Commit())

	var taskName string
	require.NoError(t, db.QueryRow(`SELECT task_name FROM forge_jobs`).Scan(&taskName))
	require.Equal(t, "tx_task", taskName)
}

func TestInsertTx_Rollback(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)
	ctx := context.Background()

	tx, err := db.Begin()
	require.NoError(t, err)

	ji := &job.JobInsert{TaskName: "rollback_task"}
	require.NoError(t, d.InsertTx(ctx, tx, ji))
	require.NoError(t, tx.Rollback())

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM forge_jobs`).Scan(&count))
	require.Equal(t, 0, count)
}

func TestInsertTx_InvalidTx(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	ji := &job.JobInsert{TaskName: "test"}
	err := d.InsertTx(context.Background(), "not-a-tx", ji)
	require.ErrorIs(t, err, job.ErrInvalidTx)
}

func TestStart_AlreadyStarted(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(50*time.Millisecond))
	ctx := context.Background()

	cfg := job.WorkerConfig{
		Executor: func(context.Context, string, json.RawMessage) error { return nil },
		Queues:   map[string]int{"default": 1},
	}

	require.NoError(t, d.Start(ctx, cfg))
	t.Cleanup(func() { _ = d.Stop(context.Background()) })

	err := d.Start(ctx, cfg)
	require.ErrorIs(t, err, job.ErrAlreadyStarted)
}

func TestStop_NotStarted(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	err := d.Stop(context.Background())
	require.ErrorIs(t, err, job.ErrNotStarted)
}

func TestHealthcheck_Success(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	require.NoError(t, d.Healthcheck(context.Background()))
}

func TestHealthcheck_ClosedDB(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.Close()

	d := New(db)
	require.Error(t, d.Healthcheck(context.Background()))
}

func TestStart_InvalidCronSchedule(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)

	cfg := job.WorkerConfig{
		Executor: func(context.Context, string, json.RawMessage) error { return nil },
		Queues:   map[string]int{"default": 1},
		PeriodicJobs: []job.PeriodicJobConfig{
			{TaskName: "bad_cron", Schedule: "not a cron expression"},
		},
	}

	err := d.Start(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cron schedule")
}

func TestRecoverOrphanedJobs(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db)
	ctx := context.Background()

	// Insert a job and mark it as running (simulating a crash).
	_, err := db.Exec(
		`INSERT INTO forge_jobs (task_name, status, started_at) VALUES ('orphan', 'running', '2024-01-01T00:00:00Z')`,
	)
	require.NoError(t, err)

	require.NoError(t, d.recoverOrphanedJobs(ctx))

	var status string
	var startedAt sql.NullString
	require.NoError(t, db.QueryRow(`SELECT status, started_at FROM forge_jobs`).Scan(&status, &startedAt))
	require.Equal(t, "pending", status)
	require.False(t, startedAt.Valid)
}
