package job

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
	"github.com/robfig/cron/v3"
)

const (
	defaultMaxWorkers = 100
	defaultQueue      = river.QueueDefault
)

// Manager handles background job processing using River.
// It combines enqueueing and worker processing capabilities.
// Manager embeds Enqueuer for job enqueueing methods.
type Manager struct {
	*Enqueuer
	registry *taskRegistry
	workers  *river.Workers
	logger   *slog.Logger

	mu      sync.Mutex
	started bool
}

// NewManager creates a new job manager with the given options.
// The River client is created immediately, allowing jobs to be enqueued
// before Start() is called. Call Start() to begin processing jobs.
func NewManager(pool *pgxpool.Pool, opts ...Option) (*Manager, error) {
	if pool == nil {
		return nil, ErrPoolRequired
	}

	cfg := newConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.logger == nil {
		cfg.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	if cfg.maxWorkers == 0 {
		cfg.maxWorkers = defaultMaxWorkers
	}

	queues := map[string]river.QueueConfig{
		defaultQueue: {MaxWorkers: cfg.maxWorkers},
	}
	for name, workers := range cfg.queues {
		queues[name] = river.QueueConfig{MaxWorkers: workers}
	}

	var periodicJobs []*river.PeriodicJob
	for _, sched := range cfg.schedules {
		cronSchedule, err := parseCronSchedule(sched.schedule)
		if err != nil {
			return nil, fmt.Errorf("job: invalid cron schedule %q: %w", sched.schedule, err)
		}

		periodicJobs = append(periodicJobs, river.NewPeriodicJob(
			cronSchedule,
			func() (river.JobArgs, *river.InsertOpts) {
				return &forgeTaskArgs{
					TaskName: sched.name,
					Payload:  nil,
				}, nil
			},
			&river.PeriodicJobOpts{
				RunOnStart: false,
			},
		))

		cfg.registry.register(sched.name, &scheduledTaskExecutor{
			handler: sched.handler,
		})
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &forgeTaskWorker{
		registry: cfg.registry,
		logger:   cfg.logger,
	})

	// Client created immediately, allowing enqueue() before Start().
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:       queues,
		Workers:      workers,
		PeriodicJobs: periodicJobs,
		Logger:       cfg.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("job: create client: %w", err)
	}

	return &Manager{
		Enqueuer: &Enqueuer{
			pool:   pool,
			client: client,
			logger: cfg.logger,
		},
		registry: cfg.registry,
		workers:  workers,
		logger:   cfg.logger,
	}, nil
}

// Start begins processing jobs.
// This should be called when the application starts.
// Jobs can be enqueued before Start() is called.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return ErrAlreadyStarted
	}

	if err := m.client.Start(ctx); err != nil {
		return fmt.Errorf("job: start client: %w", err)
	}

	m.started = true
	m.logger.Info("job manager started",
		slog.Int("tasks", len(m.registry.names())),
	)

	return nil
}

// Stop gracefully shuts down the job manager.
// It waits for currently executing jobs to complete.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return ErrNotStarted
	}

	if err := m.client.Stop(ctx); err != nil {
		return fmt.Errorf("job: stop client: %w", err)
	}

	m.started = false
	m.logger.Info("job manager stopped")
	return nil
}

// Enqueue adds a job to the queue for processing.
// The job will be executed by a registered task handler.
// Jobs can be enqueued before Start() is called; they will be processed
// once the manager starts.
func (m *Manager) Enqueue(ctx context.Context, name string, payload any, opts ...EnqueueOption) error {
	if _, ok := m.registry.get(name); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownTask, name)
	}
	return m.Enqueuer.Enqueue(ctx, name, payload, opts...)
}

// EnqueueTx adds a job to the queue within a transaction.
// The job is only visible after the transaction commits.
// This ensures atomicity between database changes and job enqueueing.
// Jobs can be enqueued before Start() is called; they will be processed
// once the manager starts.
func (m *Manager) EnqueueTx(ctx context.Context, tx pgx.Tx, name string, payload any, opts ...EnqueueOption) error {
	if _, ok := m.registry.get(name); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownTask, name)
	}
	return m.Enqueuer.EnqueueTx(ctx, tx, name, payload, opts...)
}

// forgeTaskArgs is the River job arguments type for all Forge tasks.
// It uses a unified format with task name and JSON payload.
type forgeTaskArgs struct {
	TaskName  string          `json:"task_name"`
	UniqueKey string          `json:"unique_key,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func (forgeTaskArgs) Kind() string {
	return "forge:task"
}

// forgeTaskWorker processes all Forge tasks through the registry.
type forgeTaskWorker struct {
	river.WorkerDefaults[forgeTaskArgs]
	registry *taskRegistry
	logger   *slog.Logger
}

func (w *forgeTaskWorker) Work(ctx context.Context, job *river.Job[forgeTaskArgs]) error {
	executor, ok := w.registry.get(job.Args.TaskName)
	if !ok || executor == nil {
		return fmt.Errorf("%w: %s", ErrUnknownTask, job.Args.TaskName)
	}

	w.logger.DebugContext(ctx, "executing task",
		slog.String("task", job.Args.TaskName),
		slog.Int64("job_id", job.ID),
		slog.Int("attempt", job.Attempt),
	)

	if err := executor.Execute(ctx, job.Args.Payload); err != nil {
		w.logger.ErrorContext(ctx, "task failed",
			slog.String("task", job.Args.TaskName),
			slog.Int64("job_id", job.ID),
			slog.Int("attempt", job.Attempt),
			slog.Any("error", err),
		)
		return err
	}

	w.logger.DebugContext(ctx, "task completed",
		slog.String("task", job.Args.TaskName),
		slog.Int64("job_id", job.ID),
	)

	return nil
}

type scheduledTaskExecutor struct {
	handler scheduledHandler
}

func (e *scheduledTaskExecutor) Execute(ctx context.Context, _ json.RawMessage) error {
	return e.handler(ctx)
}

type cronScheduleAdapter struct {
	schedule cron.Schedule
}

func (a *cronScheduleAdapter) Next(current time.Time) time.Time {
	return a.schedule.Next(current)
}

func parseCronSchedule(expr string) (river.PeriodicSchedule, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(expr)
	if err != nil {
		return nil, err
	}
	return &cronScheduleAdapter{schedule: schedule}, nil
}

// Shutdown returns a shutdown function for the job manager.
func (m *Manager) Shutdown() func(context.Context) error {
	return func(ctx context.Context) error {
		return m.Stop(ctx)
	}
}

// StartFunc returns a startup function for the job manager.
func (m *Manager) StartFunc() func(context.Context) error {
	return func(ctx context.Context) error {
		return m.Start(ctx)
	}
}
