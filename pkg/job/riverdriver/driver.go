package riverdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/robfig/cron/v3"

	"github.com/dmitrymomot/forge/pkg/job"
)

// RiverDriver implements job.Driver using River with PostgreSQL.
type RiverDriver struct {
	pool   *pgxpool.Pool
	logger *slog.Logger

	// insertClient is created in New() for immediate enqueue capability.
	insertClient *river.Client[pgx.Tx]

	// workerClient is created in Start() with full worker configuration.
	workerClient *river.Client[pgx.Tx]

	mu      sync.Mutex
	started bool
}

// New creates a new RiverDriver backed by the given pgxpool.Pool.
// The driver creates an insert-only River client immediately,
// allowing jobs to be enqueued before Start() is called.
func New(pool *pgxpool.Pool, opts ...Option) *RiverDriver {
	d := &RiverDriver{
		pool:   pool,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

// Migrate runs all pending River schema migrations.
// Safe to call repeatedly — already-applied migrations are skipped.
func (d *RiverDriver) Migrate(ctx context.Context) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(d.pool), nil)
	if err != nil {
		return fmt.Errorf("riverdriver: create migrator: %w", err)
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("riverdriver: migrate: %w", err)
	}
	return nil
}

// Insert adds a job to the queue.
func (d *RiverDriver) Insert(ctx context.Context, j *job.JobInsert) error {
	client, err := d.getInsertClient()
	if err != nil {
		return err
	}

	args, insertOpts := translateJobInsert(j)
	_, err = client.Insert(ctx, args, insertOpts)
	if err != nil {
		return fmt.Errorf("riverdriver: insert: %w", err)
	}
	return nil
}

// InsertTx adds a job within a pgx.Tx transaction.
// Returns job.ErrInvalidTx if tx is not a pgx.Tx.
func (d *RiverDriver) InsertTx(ctx context.Context, tx any, j *job.JobInsert) error {
	pgxTx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("%w: expected pgx.Tx, got %T", job.ErrInvalidTx, tx)
	}

	client, err := d.getInsertClient()
	if err != nil {
		return err
	}

	args, insertOpts := translateJobInsert(j)
	_, err = client.InsertTx(ctx, pgxTx, args, insertOpts)
	if err != nil {
		return fmt.Errorf("riverdriver: insert tx: %w", err)
	}
	return nil
}

// Start begins processing jobs with the given worker configuration.
func (d *RiverDriver) Start(ctx context.Context, cfg job.WorkerConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return job.ErrAlreadyStarted
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &forgeTaskWorker{
		executor: cfg.Executor,
		logger:   cfg.Logger,
	})

	queues := make(map[string]river.QueueConfig, len(cfg.Queues))
	for name, maxWorkers := range cfg.Queues {
		queues[name] = river.QueueConfig{MaxWorkers: maxWorkers}
	}

	var periodicJobs []*river.PeriodicJob
	for _, pj := range cfg.PeriodicJobs {
		schedule, err := parseCronSchedule(pj.Schedule)
		if err != nil {
			return fmt.Errorf("riverdriver: invalid cron schedule %q: %w", pj.Schedule, err)
		}

		taskName := pj.TaskName
		periodicJobs = append(periodicJobs, river.NewPeriodicJob(
			schedule,
			func() (river.JobArgs, *river.InsertOpts) {
				return &forgeTaskArgs{TaskName: taskName}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: false},
		))
	}

	client, err := river.NewClient(riverpgxv5.New(d.pool), &river.Config{
		Queues:       queues,
		Workers:      workers,
		PeriodicJobs: periodicJobs,
		Logger:       cfg.Logger,
	})
	if err != nil {
		return fmt.Errorf("riverdriver: create worker client: %w", err)
	}

	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("riverdriver: start: %w", err)
	}

	d.workerClient = client
	d.started = true
	return nil
}

// Stop gracefully shuts down job processing.
func (d *RiverDriver) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.started {
		return job.ErrNotStarted
	}

	if err := d.workerClient.Stop(ctx); err != nil {
		return fmt.Errorf("riverdriver: stop: %w", err)
	}

	d.started = false
	return nil
}

// Healthcheck verifies PostgreSQL connectivity.
func (d *RiverDriver) Healthcheck(ctx context.Context) error {
	return d.pool.Ping(ctx)
}

// getInsertClient returns the insert-only client, creating it lazily if needed.
func (d *RiverDriver) getInsertClient() (*river.Client[pgx.Tx], error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.insertClient != nil {
		return d.insertClient, nil
	}

	client, err := river.NewClient(riverpgxv5.New(d.pool), &river.Config{
		Logger: d.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("riverdriver: create insert client: %w", err)
	}

	d.insertClient = client
	return client, nil
}

// forgeTaskArgs is the River job arguments type for all Forge tasks.
type forgeTaskArgs struct {
	TaskName  string          `json:"task_name"`
	UniqueKey string          `json:"unique_key,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func (forgeTaskArgs) Kind() string {
	return "forge:task"
}

// forgeTaskWorker processes all Forge tasks via the executor callback.
type forgeTaskWorker struct {
	river.WorkerDefaults[forgeTaskArgs]
	executor func(ctx context.Context, taskName string, payload json.RawMessage) error
	logger   *slog.Logger
}

func (w *forgeTaskWorker) Work(ctx context.Context, j *river.Job[forgeTaskArgs]) error {
	w.logger.DebugContext(ctx, "executing task",
		slog.String("task", j.Args.TaskName),
		slog.Int64("job_id", j.ID),
		slog.Int("attempt", j.Attempt),
	)

	if err := w.executor(ctx, j.Args.TaskName, j.Args.Payload); err != nil {
		w.logger.ErrorContext(ctx, "task failed",
			slog.String("task", j.Args.TaskName),
			slog.Int64("job_id", j.ID),
			slog.Int("attempt", j.Attempt),
			slog.Any("error", err),
		)
		return err
	}

	w.logger.DebugContext(ctx, "task completed",
		slog.String("task", j.Args.TaskName),
		slog.Int64("job_id", j.ID),
	)
	return nil
}

// translateJobInsert converts a job.JobInsert into River-specific types.
func translateJobInsert(j *job.JobInsert) (*forgeTaskArgs, *river.InsertOpts) {
	args := &forgeTaskArgs{
		TaskName:  j.TaskName,
		Payload:   j.Payload,
		UniqueKey: j.UniqueKey,
	}

	insertOpts := &river.InsertOpts{}
	if j.Queue != "" {
		insertOpts.Queue = j.Queue
	}
	if j.ScheduledAt != nil {
		insertOpts.ScheduledAt = *j.ScheduledAt
	}
	if j.MaxAttempts > 0 {
		insertOpts.MaxAttempts = j.MaxAttempts
	}
	if j.Priority > 0 {
		insertOpts.Priority = j.Priority
	}
	if len(j.Tags) > 0 {
		insertOpts.Tags = j.Tags
	}
	if j.UniqueFor > 0 {
		insertOpts.UniqueOpts = river.UniqueOpts{
			ByPeriod: j.UniqueFor,
		}
	}

	return args, insertOpts
}

// cronScheduleAdapter adapts robfig/cron to River's PeriodicSchedule.
type cronScheduleAdapter struct {
	schedule cron.Schedule
}

func (a *cronScheduleAdapter) Next(current time.Time) time.Time {
	return a.schedule.Next(current)
}

func parseCronSchedule(expr string) (river.PeriodicSchedule, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	schedule, err := parser.Parse(expr)
	if err != nil {
		return nil, err
	}
	return &cronScheduleAdapter{schedule: schedule}, nil
}
