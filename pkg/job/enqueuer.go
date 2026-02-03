package job

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Enqueuer provides job enqueueing without worker processing.
// Use this for applications that only need to dispatch jobs to be processed
// by separate worker processes.
type Enqueuer struct {
	pool   *pgxpool.Pool
	client *river.Client[pgx.Tx]
	logger *slog.Logger
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
// The River client is created in insert-only mode (no workers).
func NewEnqueuer(pool *pgxpool.Pool, opts ...EnqueuerOption) (*Enqueuer, error) {
	if pool == nil {
		return nil, ErrPoolRequired
	}

	cfg := &enqueuerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.logger == nil {
		cfg.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	// Create River client in insert-only mode (no Workers, no Queues)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: cfg.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("job: create enqueuer client: %w", err)
	}

	return &Enqueuer{
		pool:   pool,
		client: client,
		logger: cfg.logger,
	}, nil
}

// Enqueue adds a job to the queue for processing by workers.
// The job will be executed by a registered task handler on a worker process.
// Note: Task name validation happens on the worker side.
func (e *Enqueuer) Enqueue(ctx context.Context, name string, payload any, opts ...EnqueueOption) error {
	args, insertOpts, err := buildJobArgs(name, payload, opts...)
	if err != nil {
		return err
	}

	_, err = e.client.Insert(ctx, args, insertOpts)
	if err != nil {
		return fmt.Errorf("job: enqueue: %w", err)
	}

	return nil
}

// EnqueueTx adds a job to the queue within a transaction.
// The job is only visible after the transaction commits.
// This ensures atomicity between database changes and job enqueueing.
func (e *Enqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, name string, payload any, opts ...EnqueueOption) error {
	args, insertOpts, err := buildJobArgs(name, payload, opts...)
	if err != nil {
		return err
	}

	_, err = e.client.InsertTx(ctx, tx, args, insertOpts)
	if err != nil {
		return fmt.Errorf("job: enqueue tx: %w", err)
	}

	return nil
}

// buildJobArgs creates River job arguments from the task name and payload.
// This is shared between Enqueuer and Manager.
func buildJobArgs(name string, payload any, opts ...EnqueueOption) (*forgeTaskArgs, *river.InsertOpts, error) {
	var payloadBytes json.RawMessage
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("job: marshal payload: %w", err)
		}
	}

	args := &forgeTaskArgs{
		TaskName: name,
		Payload:  payloadBytes,
	}

	enqCfg := &enqueueConfig{}
	for _, opt := range opts {
		opt(enqCfg)
	}

	insertOpts := &river.InsertOpts{}
	if enqCfg.queue != "" {
		insertOpts.Queue = enqCfg.queue
	}
	if enqCfg.scheduledAt != nil {
		insertOpts.ScheduledAt = *enqCfg.scheduledAt
	}
	if enqCfg.maxAttempts > 0 {
		insertOpts.MaxAttempts = enqCfg.maxAttempts
	}
	if enqCfg.priority > 0 {
		insertOpts.Priority = enqCfg.priority
	}
	if len(enqCfg.tags) > 0 {
		insertOpts.Tags = enqCfg.tags
	}
	if enqCfg.uniqueFor > 0 {
		insertOpts.UniqueOpts = river.UniqueOpts{
			ByPeriod: enqCfg.uniqueFor,
		}
		if enqCfg.uniqueKey != "" {
			args.UniqueKey = enqCfg.uniqueKey
		}
	}

	return args, insertOpts, nil
}
