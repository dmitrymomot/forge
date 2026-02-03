package internal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/pkg/job"
)

// JobManager wraps the pkg/job.Manager for internal use.
// It provides the integration between the framework and the job package.
type JobManager struct {
	manager *job.Manager
}

// NewJobManager creates a new JobManager with the given pool and options.
func NewJobManager(pool *pgxpool.Pool, opts ...job.Option) (*JobManager, error) {
	m, err := job.NewManager(pool, opts...)
	if err != nil {
		return nil, err
	}
	return &JobManager{manager: m}, nil
}

// Start begins job processing.
func (jm *JobManager) Start(ctx context.Context) error {
	return jm.manager.Start(ctx)
}

// Stop gracefully shuts down job processing.
func (jm *JobManager) Stop(ctx context.Context) error {
	return jm.manager.Stop(ctx)
}

// Enqueue adds a job to the queue.
func (jm *JobManager) Enqueue(ctx context.Context, name string, payload any, opts ...job.EnqueueOption) error {
	return jm.manager.Enqueue(ctx, name, payload, opts...)
}

// EnqueueTx adds a job to the queue within a transaction.
func (jm *JobManager) EnqueueTx(ctx context.Context, tx pgx.Tx, name string, payload any, opts ...job.EnqueueOption) error {
	return jm.manager.EnqueueTx(ctx, tx, name, payload, opts...)
}

// Manager returns the underlying job.Manager.
func (jm *JobManager) Manager() *job.Manager {
	return jm.manager
}

// Shutdown returns a shutdown function for the job manager.
func (jm *JobManager) Shutdown() func(context.Context) error {
	return jm.manager.Shutdown()
}
