package job

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"sync"
)

const (
	defaultMaxWorkers = 100
	defaultQueue      = "default"
)

// Manager handles background job processing.
// It combines enqueueing and worker processing capabilities.
// Manager embeds Enqueuer for job enqueueing methods.
type Manager struct {
	*Enqueuer
	driver    Driver
	registry  *taskRegistry
	logger    *slog.Logger
	queues    map[string]int
	schedules []scheduleConfig

	maxWorkersCfg int
	mu            sync.Mutex
	started       bool
}

// NewManager creates a new job manager with the given driver, config, and options.
// Jobs can be enqueued immediately via the embedded Enqueuer.
// Call Start() to begin processing jobs.
func NewManager(driver Driver, userCfg Config, opts ...Option) (*Manager, error) {
	if driver == nil {
		return nil, ErrDriverRequired
	}

	cfg := newConfig()
	cfg.maxWorkers = userCfg.MaxWorkers
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.logger == nil {
		cfg.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	if cfg.maxWorkers == 0 {
		cfg.maxWorkers = defaultMaxWorkers
	}

	// Register scheduled task executors in the registry.
	for _, sched := range cfg.schedules {
		cfg.registry.register(sched.name, &scheduledTaskExecutor{
			handler: sched.handler,
		})
	}

	return &Manager{
		Enqueuer: &Enqueuer{
			driver: driver,
			logger: cfg.logger,
		},
		driver:        driver,
		registry:      cfg.registry,
		logger:        cfg.logger,
		queues:        cfg.queues,
		schedules:     cfg.schedules,
		maxWorkersCfg: cfg.maxWorkers,
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

	// Build queues map for the driver.
	queues := map[string]int{
		defaultQueue: m.maxWorkersCfg,
	}
	maps.Copy(queues, m.queues)

	// Build periodic jobs config.
	var periodicJobs []PeriodicJobConfig
	for _, sched := range m.schedules {
		periodicJobs = append(periodicJobs, PeriodicJobConfig{
			TaskName: sched.name,
			Schedule: sched.schedule,
		})
	}

	// Create executor that routes through the task registry.
	executor := func(ctx context.Context, taskName string, payload json.RawMessage) error {
		exec, ok := m.registry.get(taskName)
		if !ok || exec == nil {
			return fmt.Errorf("%w: %s", ErrUnknownTask, taskName)
		}
		return exec.Execute(ctx, payload)
	}

	wcfg := WorkerConfig{
		Executor:     executor,
		Queues:       queues,
		PeriodicJobs: periodicJobs,
		Logger:       m.logger,
	}

	if err := m.driver.Start(ctx, wcfg); err != nil {
		return fmt.Errorf("job: start: %w", err)
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

	if err := m.driver.Stop(ctx); err != nil {
		return fmt.Errorf("job: stop: %w", err)
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
// The tx type depends on the driver (pgx.Tx for River, *sql.Tx for SQLite).
// Jobs can be enqueued before Start() is called; they will be processed
// once the manager starts.
func (m *Manager) EnqueueTx(ctx context.Context, tx any, name string, payload any, opts ...EnqueueOption) error {
	if _, ok := m.registry.get(name); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownTask, name)
	}
	return m.Enqueuer.EnqueueTx(ctx, tx, name, payload, opts...)
}

type scheduledTaskExecutor struct {
	handler scheduledHandler
}

func (e *scheduledTaskExecutor) Execute(ctx context.Context, _ json.RawMessage) error {
	return e.handler(ctx)
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
