package sqlitedriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// resultWriteTimeout bounds the final status write for a finished job. It uses
// a context detached from the poller's lifecycle so a job that completes during
// Stop can still persist its result, while preventing an indefinite hang.
const resultWriteTimeout = 5 * time.Second

// pollQueue polls for pending jobs in the given queue and dispatches them
// to the executor. It uses a semaphore to limit concurrency.
func (d *SQLiteDriver) pollQueue(
	ctx context.Context,
	queue string,
	concurrency int,
	executor func(ctx context.Context, taskName string, payload json.RawMessage) error,
	logger *slog.Logger,
) {
	sem := make(chan struct{}, concurrency)
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Try to acquire a semaphore slot (non-blocking).
			select {
			case sem <- struct{}{}:
			default:
				continue // all workers busy, skip this tick
			}

			id, taskName, payload, err := d.claimJob(ctx, queue)
			if err != nil {
				<-sem
				if ctx.Err() != nil {
					return
				}
				logger.ErrorContext(ctx, "poll error", slog.String("queue", queue), slog.Any("error", err))
				continue
			}
			if id == 0 {
				<-sem // no job available
				continue
			}

			d.wg.Go(func() {
				defer func() { <-sem }()
				d.executeJob(ctx, id, taskName, payload, executor, logger)
			})
		}
	}
}

// claimJob atomically selects and claims the next pending job for the given queue.
// Returns (0, "", nil, nil) if no job is available.
func (d *SQLiteDriver) claimJob(ctx context.Context, queue string) (int64, string, json.RawMessage, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", nil, fmt.Errorf("sqlitedriver: begin claim tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		id       int64
		taskName string
		payload  sql.NullString
	)

	err = tx.QueryRowContext(ctx,
		`SELECT id, task_name, payload FROM forge_jobs
		 WHERE queue = ? AND status = 'pending' AND scheduled_at <= ? AND attempt < max_attempts
		 ORDER BY priority ASC, id ASC
		 LIMIT 1`,
		queue, now,
	).Scan(&id, &taskName, &payload)
	if err == sql.ErrNoRows {
		return 0, "", nil, nil
	}
	if err != nil {
		return 0, "", nil, fmt.Errorf("sqlitedriver: select job: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE forge_jobs SET status = 'running', attempt = attempt + 1, started_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return 0, "", nil, fmt.Errorf("sqlitedriver: claim job: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, "", nil, fmt.Errorf("sqlitedriver: commit claim: %w", err)
	}

	var rawPayload json.RawMessage
	if payload.Valid {
		rawPayload = json.RawMessage(payload.String)
	}

	return id, taskName, rawPayload, nil
}

// executeJob runs the executor for a claimed job and updates its status.
func (d *SQLiteDriver) executeJob(
	ctx context.Context,
	jobID int64,
	taskName string,
	payload json.RawMessage,
	executor func(ctx context.Context, taskName string, payload json.RawMessage) error,
	logger *slog.Logger,
) {
	logger.DebugContext(ctx, "executing task",
		slog.String("task", taskName),
		slog.Int64("job_id", jobID),
	)

	execErr := d.safeExecute(ctx, taskName, payload, executor)

	// Persist the result using a context detached from the poller's lifecycle.
	// On Stop the poller ctx is cancelled, but a job that already finished must
	// still record its terminal status — otherwise it would be wrongly recovered
	// as "orphaned" on the next start. A short timeout bounds the write so Stop
	// cannot hang indefinitely.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resultWriteTimeout)
	defer cancel()

	now := time.Now().UTC().Format(time.RFC3339Nano)

	if execErr == nil {
		// Success → completed.
		_, err := d.db.ExecContext(writeCtx,
			`UPDATE forge_jobs SET status = 'completed', completed_at = ? WHERE id = ?`,
			now, jobID,
		)
		if err != nil {
			logger.ErrorContext(writeCtx, "failed to mark job completed",
				slog.Int64("job_id", jobID), slog.Any("error", err))
		}
		logger.DebugContext(writeCtx, "task completed",
			slog.String("task", taskName), slog.Int64("job_id", jobID))
		return
	}

	logger.ErrorContext(writeCtx, "task failed",
		slog.String("task", taskName),
		slog.Int64("job_id", jobID),
		slog.Any("error", execErr),
	)

	// Check if retries remain.
	var attempt, maxAttempts int
	err := d.db.QueryRowContext(writeCtx,
		`SELECT attempt, max_attempts FROM forge_jobs WHERE id = ?`, jobID,
	).Scan(&attempt, &maxAttempts)
	if err != nil {
		logger.ErrorContext(writeCtx, "failed to read job attempts",
			slog.Int64("job_id", jobID), slog.Any("error", err))
		return
	}

	if attempt >= maxAttempts {
		// No retries left → discarded.
		_, err = d.db.ExecContext(writeCtx,
			`UPDATE forge_jobs SET status = 'discarded', completed_at = ? WHERE id = ?`,
			now, jobID,
		)
	} else {
		// Retries left → back to pending.
		_, err = d.db.ExecContext(writeCtx,
			`UPDATE forge_jobs SET status = 'pending', started_at = NULL WHERE id = ?`,
			jobID,
		)
	}
	if err != nil {
		logger.ErrorContext(writeCtx, "failed to update job status after failure",
			slog.Int64("job_id", jobID), slog.Any("error", err))
	}
}

// safeExecute runs the executor and recovers from panics.
func (d *SQLiteDriver) safeExecute(
	ctx context.Context,
	taskName string,
	payload json.RawMessage,
	executor func(ctx context.Context, taskName string, payload json.RawMessage) error,
) (execErr error) {
	defer func() {
		if r := recover(); r != nil {
			execErr = fmt.Errorf("sqlitedriver: task panicked: %v", r)
		}
	}()
	return executor(ctx, taskName, payload)
}
