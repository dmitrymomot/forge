package internal

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/pkg/job"
)

// JobManager wraps the pkg/job.Manager for internal use in the framework.
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

// Manager returns the underlying job.Manager.
func (jm *JobManager) Manager() *job.Manager {
	return jm.manager
}

// Shutdown returns a shutdown function for the job manager.
func (jm *JobManager) Shutdown() func(context.Context) error {
	return jm.manager.Shutdown()
}
