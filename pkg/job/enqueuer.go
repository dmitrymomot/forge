package job

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// Enqueuer provides job enqueueing without worker processing.
// Use this for applications that only need to dispatch jobs to be processed
// by separate worker processes.
type Enqueuer struct {
	driver Driver
	logger *slog.Logger

	// registry is optional. When set (i.e. the Enqueuer is owned by a Manager
	// that knows which tasks are registered), Enqueue/EnqueueTx validate the
	// task name against it and return ErrUnknownTask for unregistered tasks.
	// A standalone Enqueuer (worker runs elsewhere) leaves this nil and defers
	// task-name validation to the worker side.
	registry *taskRegistry
}

// EnqueuerOption configures the enqueuer.
type EnqueuerOption func(*enqueuerConfig)

type enqueuerConfig struct {
	logger *slog.Logger
}

// WithEnqueuerLogger sets the logger for the enqueuer.
func WithEnqueuerLogger(l *slog.Logger) EnqueuerOption {
	return func(c *enqueuerConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// NewEnqueuer creates a new enqueue-only client.
// The driver is used directly for inserting jobs without worker processing.
func NewEnqueuer(driver Driver, opts ...EnqueuerOption) (*Enqueuer, error) {
	if driver == nil {
		return nil, ErrDriverRequired
	}

	cfg := &enqueuerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.logger == nil {
		cfg.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &Enqueuer{
		driver: driver,
		logger: cfg.logger,
	}, nil
}

// validateTask returns ErrUnknownTask if a registry is configured and the named
// task is not registered. A nil registry (standalone enqueuer) is a no-op,
// deferring validation to the worker side.
func (e *Enqueuer) validateTask(name string) error {
	if e.registry == nil {
		return nil
	}
	if _, ok := e.registry.get(name); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownTask, name)
	}
	return nil
}

// Enqueue adds a job to the queue for processing by workers.
// The job will be executed by a registered task handler on a worker process.
// When the enqueuer is owned by a Manager, unknown task names are rejected with
// ErrUnknownTask; for a standalone enqueuer, task-name validation happens on the
// worker side.
func (e *Enqueuer) Enqueue(ctx context.Context, name string, payload any, opts ...EnqueueOption) error {
	if err := e.validateTask(name); err != nil {
		return err
	}

	ji, err := buildJobInsert(name, payload, opts...)
	if err != nil {
		return err
	}

	if err := e.driver.Insert(ctx, ji); err != nil {
		return fmt.Errorf("job: enqueue: %w", err)
	}

	return nil
}

// EnqueueTx adds a job to the queue within a transaction.
// The job is only visible after the transaction commits.
// This ensures atomicity between database changes and job enqueueing.
// The tx type depends on the driver (pgx.Tx for River, *sql.Tx for SQLite).
func (e *Enqueuer) EnqueueTx(ctx context.Context, tx any, name string, payload any, opts ...EnqueueOption) error {
	if err := e.validateTask(name); err != nil {
		return err
	}

	ji, err := buildJobInsert(name, payload, opts...)
	if err != nil {
		return err
	}

	if err := e.driver.InsertTx(ctx, tx, ji); err != nil {
		return fmt.Errorf("job: enqueue tx: %w", err)
	}

	return nil
}

// buildJobInsert creates a driver-agnostic JobInsert from the task name, payload, and options.
func buildJobInsert(name string, payload any, opts ...EnqueueOption) (*JobInsert, error) {
	var payloadBytes json.RawMessage
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("job: marshal payload: %w", err)
		}
	}

	enqCfg := &enqueueConfig{}
	for _, opt := range opts {
		opt(enqCfg)
	}

	ji := &JobInsert{
		TaskName: name,
		Payload:  payloadBytes,
		Queue:    enqCfg.queue,
	}

	if enqCfg.scheduledAt != nil {
		ji.ScheduledAt = enqCfg.scheduledAt
	}
	if enqCfg.maxAttempts > 0 {
		ji.MaxAttempts = enqCfg.maxAttempts
	}
	if enqCfg.priority > 0 {
		ji.Priority = enqCfg.priority
	}
	if len(enqCfg.tags) > 0 {
		ji.Tags = enqCfg.tags
	}
	// UniqueKey is always forwarded to the driver, independent of UniqueFor.
	// Deduplication only takes effect when UniqueFor > 0 (it defines the window),
	// but forwarding the key unconditionally avoids silently dropping a
	// caller-supplied WithUniqueKey and lets drivers persist it for inspection.
	if enqCfg.uniqueKey != "" {
		ji.UniqueKey = enqCfg.uniqueKey
	}
	if enqCfg.uniqueFor > 0 {
		ji.UniqueFor = enqCfg.uniqueFor
	}

	return ji, nil
}
