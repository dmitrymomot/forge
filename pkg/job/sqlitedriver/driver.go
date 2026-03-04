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

CREATE INDEX IF NOT EXISTS idx_forge_jobs_unique
    ON forge_jobs (task_name, unique_key)
    WHERE unique_key != '' AND status NOT IN ('completed', 'discarded', 'failed');
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

	// Dedup check: if UniqueKey is set and UniqueFor > 0,
	// skip if an active job with the same task_name+unique_key exists within the window.
	if uniqueKey != "" && j.UniqueFor > 0 {
		cutoff := time.Now().UTC().Add(-j.UniqueFor).Format(time.RFC3339Nano)
		var count int
		err := qe.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM forge_jobs
			 WHERE task_name = ? AND unique_key = ?
			   AND status NOT IN ('completed', 'discarded', 'failed')
			   AND created_at >= ?`,
			j.TaskName, uniqueKey, cutoff,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("sqlitedriver: dedup check: %w", err)
		}
		if count > 0 {
			return nil // skip duplicate
		}
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

	_, err := qe.ExecContext(ctx,
		`INSERT INTO forge_jobs (queue, task_name, payload, unique_key, tags, priority, max_attempts, scheduled_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		queue, j.TaskName, payload, uniqueKey, tagsJSON, priority, maxAttempts, scheduledAt,
	)
	if err != nil {
		return fmt.Errorf("sqlitedriver: insert: %w", err)
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
