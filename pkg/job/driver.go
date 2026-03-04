package job

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// Driver is the interface that job queue backends must implement.
// Implementations include riverdriver (PostgreSQL via River) and
// sqlitedriver (SQLite-based polling queue).
type Driver interface {
	// Migrate runs schema migrations for the job queue backend.
	// Must be idempotent — safe to call repeatedly.
	Migrate(ctx context.Context) error

	// Insert adds a job to the queue.
	Insert(ctx context.Context, j *JobInsert) error

	// InsertTx adds a job within a backend-specific transaction.
	// The tx type depends on the driver: pgx.Tx for River, *sql.Tx for SQLite.
	// Returns ErrInvalidTx if the wrong transaction type is provided.
	InsertTx(ctx context.Context, tx any, j *JobInsert) error

	// Start begins processing jobs with the given worker configuration.
	// Only called in manager/worker mode, not for enqueue-only use.
	Start(ctx context.Context, cfg WorkerConfig) error

	// Stop gracefully shuts down job processing.
	// Should wait for in-flight jobs to complete or until ctx is cancelled.
	Stop(ctx context.Context) error

	// Healthcheck verifies backend connectivity.
	Healthcheck(ctx context.Context) error
}

// JobInsert describes a job to be inserted into the queue.
// This is the driver-agnostic representation that each Driver
// translates into its backend-specific format.
type JobInsert struct {
	ScheduledAt *time.Time
	TaskName    string
	UniqueKey   string
	Queue       string
	Payload     json.RawMessage
	Tags        []string
	MaxAttempts int
	Priority    int
	UniqueFor   time.Duration
}

// WorkerConfig holds the configuration passed to Driver.Start().
// It contains everything the driver needs to process jobs.
type WorkerConfig struct {
	// Executor is called for each job. The driver passes the task name
	// and raw JSON payload; routing to the correct handler is the
	// framework's responsibility (via taskRegistry).
	Executor func(ctx context.Context, taskName string, payload json.RawMessage) error

	// Queues maps queue names to their max concurrent worker count.
	Queues map[string]int

	// Logger for driver-internal logging.
	Logger *slog.Logger

	// PeriodicJobs defines cron-scheduled recurring jobs.
	PeriodicJobs []PeriodicJobConfig
}

// PeriodicJobConfig defines a recurring job fired on a cron schedule.
type PeriodicJobConfig struct {
	TaskName string
	Schedule string // cron expression (5-field, descriptors, or intervals)
}
