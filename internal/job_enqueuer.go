package internal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/pkg/job"
)

// JobEnqueuer wraps the pkg/job.Enqueuer for internal use.
// It provides enqueueing capability without worker processing.
type JobEnqueuer struct {
	enqueuer *job.Enqueuer
}

// NewJobEnqueuer creates a new JobEnqueuer with the given pool and options.
func NewJobEnqueuer(pool *pgxpool.Pool, opts ...job.EnqueuerOption) (*JobEnqueuer, error) {
	e, err := job.NewEnqueuer(pool, opts...)
	if err != nil {
		return nil, err
	}
	return &JobEnqueuer{enqueuer: e}, nil
}

// Enqueue adds a job to the queue.
func (je *JobEnqueuer) Enqueue(ctx context.Context, name string, payload any, opts ...job.EnqueueOption) error {
	return je.enqueuer.Enqueue(ctx, name, payload, opts...)
}

// EnqueueTx adds a job to the queue within a transaction.
func (je *JobEnqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, name string, payload any, opts ...job.EnqueueOption) error {
	return je.enqueuer.EnqueueTx(ctx, tx, name, payload, opts...)
}

// Enqueuer returns the underlying job.Enqueuer.
func (je *JobEnqueuer) Enqueuer() *job.Enqueuer {
	return je.enqueuer
}
