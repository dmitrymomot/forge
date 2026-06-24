package sqlitedriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/pkg/job"
)

const (
	defaultPollInterval = 1 * time.Second
	defaultQueue        = "default"
	defaultMaxAttempts  = 25
	defaultPriority     = 1
)

// SQLiteDriver implements job.Driver using SQLite as the backing store.
// It uses polling to check for pending jobs and supports multiple queues,
// automatic retries, deduplication, and cron-based periodic jobs.
type SQLiteDriver struct {
	db     *sql.DB
	logger *slog.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup

	pollInterval time.Duration

	mu sync.Mutex

	started bool
}

// New creates a new SQLiteDriver backed by the given *sql.DB.
// Jobs can be inserted immediately; call Start() to begin processing.
func New(db *sql.DB, opts ...Option) *SQLiteDriver {
	d := &SQLiteDriver{
		db:           db,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		pollInterval: defaultPollInterval,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Migrate creates the forge_jobs table and indexes.
// Safe to call repeatedly — uses IF NOT EXISTS.
func (d *SQLiteDriver) Migrate(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS forge_jobs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    queue        TEXT    NOT NULL DEFAULT 'default',
    task_name    TEXT    NOT NULL,
    payload      TEXT,
    unique_key   TEXT    NOT NULL DEFAULT '',
    dedup_key    TEXT    NOT NULL DEFAULT '',
    tags         TEXT    NOT NULL DEFAULT '[]',
    priority     INTEGER NOT NULL DEFAULT 1,
    max_attempts INTEGER NOT NULL DEFAULT 25,
    attempt      INTEGER NOT NULL DEFAULT 0,
    status       TEXT    NOT NULL DEFAULT 'pending',
    scheduled_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    started_at   TEXT,
    completed_at TEXT,
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_forge_jobs_poll
    ON forge_jobs (queue, status, scheduled_at, priority, id)
    WHERE status = 'pending';

-- Atomic deduplication: a partial UNIQUE index over dedup_key guarantees that at
-- most one *active* (non-terminal) job can exist per dedup key. dedup_key is only
-- populated for jobs that opt into dedup (UniqueFor > 0), so plain unique_key
-- usage without UniqueFor is unconstrained. Concurrent inserts that both target
-- the same active dedup key cannot both succeed — the second hits ON CONFLICT.
CREATE UNIQUE INDEX IF NOT EXISTS idx_forge_jobs_dedup
    ON forge_jobs (dedup_key)
    WHERE dedup_key != '' AND status NOT IN ('completed', 'discarded', 'failed');
`
	if _, err := d.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("sqlitedriver: migrate: %w", err)
	}
	return nil
}

// queryExecer is satisfied by both *sql.DB and *sql.Tx,
// allowing shared insert logic.
type queryExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Insert adds a job to the queue.
func (d *SQLiteDriver) Insert(ctx context.Context, j *job.JobInsert) error {
	return insertJob(ctx, d.db, j)
}

// InsertTx adds a job within a *sql.Tx transaction.
// Returns job.ErrInvalidTx if tx is not a *sql.Tx.
func (d *SQLiteDriver) InsertTx(ctx context.Context, tx any, j *job.JobInsert) error {
	sqlTx, ok := tx.(*sql.Tx)
	if !ok {
		return fmt.Errorf("%w: expected *sql.Tx, got %T", job.ErrInvalidTx, tx)
	}
	return insertJob(ctx, sqlTx, j)
}

// insertJob inserts a job using the given queryExecer (db or tx).
func insertJob(ctx context.Context, qe queryExecer, j *job.JobInsert) error {
	queue := j.Queue
	if queue == "" {
		queue = defaultQueue
	}
	maxAttempts := j.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = defaultMaxAttempts
	}
	priority := j.Priority
	if priority == 0 {
		priority = defaultPriority
	}

	uniqueKey := j.UniqueKey

	// dedupKey is non-empty only when the job opts into deduplication
	// (UniqueKey set AND UniqueFor > 0). It scopes the partial UNIQUE index so
	// that at most one *active* job can exist for this (task, key) pair. The NUL
	// separator prevents ("ab","c") and ("a","bc") from colliding.
	dedupKey := ""
	if uniqueKey != "" && j.UniqueFor > 0 {
		dedupKey = j.TaskName + "\x00" + uniqueKey
	}

	// Marshal tags.
	tagsJSON := "[]"
	if len(j.Tags) > 0 {
		b, err := json.Marshal(j.Tags)
		if err != nil {
			return fmt.Errorf("sqlitedriver: marshal tags: %w", err)
		}
		tagsJSON = string(b)
	}

	// Encode payload.
	var payload any
	if len(j.Payload) > 0 {
		payload = string(j.Payload)
	}

	// Determine scheduled_at.
	scheduledAt := time.Now().UTC().Format(time.RFC3339Nano)
	if j.ScheduledAt != nil {
		scheduledAt = j.ScheduledAt.UTC().Format(time.RFC3339Nano)
	}

	// For dedup-opted jobs, rely on the partial UNIQUE index over dedup_key:
	// ON CONFLICT ... DO NOTHING makes the check-and-insert atomic, so two
	// concurrent inserts for the same active dedup key cannot both succeed.
	// A 0-row result means an active duplicate already exists → silently skip.
	const insertSQL = `INSERT INTO forge_jobs
		(queue, task_name, payload, unique_key, dedup_key, tags, priority, max_attempts, scheduled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	const onConflict = `
		ON CONFLICT (dedup_key)
		WHERE dedup_key != '' AND status NOT IN ('completed', 'discarded', 'failed')
		DO NOTHING`

	stmt := insertSQL
	if dedupKey != "" {
		stmt += onConflict
	}

	res, err := qe.ExecContext(ctx, stmt,
		queue, j.TaskName, payload, uniqueKey, dedupKey, tagsJSON, priority, maxAttempts, scheduledAt,
	)
	if err != nil {
		return fmt.Errorf("sqlitedriver: insert: %w", err)
	}
	if dedupKey != "" {
		if n, _ := res.RowsAffected(); n == 0 {
			return nil // active duplicate exists → skipped
		}
	}
	return nil
}

// Start begins processing jobs with the given worker configuration.
func (d *SQLiteDriver) Start(ctx context.Context, cfg job.WorkerConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return job.ErrAlreadyStarted
	}

	// Parse all cron expressions up front (fail fast).
	periodicEntries, err := parsePeriodicJobs(cfg.PeriodicJobs)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	// Recover orphaned running jobs from a previous crash.
	if err := d.recoverOrphanedJobs(ctx); err != nil {
		cancel()
		return fmt.Errorf("sqlitedriver: recover orphaned jobs: %w", err)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = d.logger
	}

	// Launch a polling goroutine per queue.
	for queueName, concurrency := range cfg.Queues {
		d.wg.Go(func() {
			d.pollQueue(ctx, queueName, concurrency, cfg.Executor, logger)
		})
	}

	// Launch periodic scheduler if there are cron jobs.
	if len(periodicEntries) > 0 {
		d.wg.Go(func() {
			d.runPeriodicScheduler(ctx, periodicEntries, logger)
		})
	}

	d.started = true
	return nil
}

// Stop gracefully shuts down job processing.
// It cancels the context and waits for goroutines to finish,
// respecting the provided context's deadline.
func (d *SQLiteDriver) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.started {
		return job.ErrNotStarted
	}

	d.cancel()

	// Wait for goroutines with timeout.
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		d.started = false
		return fmt.Errorf("sqlitedriver: stop: %w", ctx.Err())
	}

	d.started = false
	return nil
}

// Healthcheck verifies SQLite connectivity.
func (d *SQLiteDriver) Healthcheck(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// recoverOrphanedJobs resets any jobs stuck in "running" status back to "pending".
// This handles the case where the process crashed while jobs were being processed.
func (d *SQLiteDriver) recoverOrphanedJobs(ctx context.Context) error {
	result, err := d.db.ExecContext(ctx,
		`UPDATE forge_jobs SET status = 'pending', started_at = NULL WHERE status = 'running'`)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		d.logger.InfoContext(ctx, "recovered orphaned jobs", slog.Int64("count", n))
	}
	return nil
}
