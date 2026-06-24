package sqlitedriver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/job"

	_ "modernc.org/sqlite"
)

func TestPollQueue_ExecutesJob(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(50*time.Millisecond))
	ctx := context.Background()

	// Insert a pending job.
	ji := &job.JobInsert{TaskName: "poll_task", Payload: json.RawMessage(`{"key":"value"}`)}
	require.NoError(t, d.Insert(ctx, ji))

	var called atomic.Bool
	var receivedTask string
	var receivedPayload json.RawMessage

	cfg := job.WorkerConfig{
		Executor: func(_ context.Context, taskName string, payload json.RawMessage) error {
			receivedTask = taskName
			receivedPayload = payload
			called.Store(true)
			return nil
		},
		Queues: map[string]int{"default": 1},
	}

	require.NoError(t, d.Start(ctx, cfg))
	defer func() { _ = d.Stop(context.Background()) }()

	// Wait for the job to be executed.
	require.Eventually(t, func() bool { return called.Load() }, 2*time.Second, 25*time.Millisecond)

	require.Equal(t, "poll_task", receivedTask)
	require.JSONEq(t, `{"key":"value"}`, string(receivedPayload))

	// Verify job is marked completed.
	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM forge_jobs`).Scan(&status))
	require.Equal(t, "completed", status)
}

func TestPollQueue_PriorityOrdering(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(50*time.Millisecond))
	ctx := context.Background()

	// Insert jobs with different priorities (lower = higher priority).
	for _, p := range []int{3, 1, 2} {
		ji := &job.JobInsert{
			TaskName: "priority_task",
			Payload:  json.RawMessage(`{"p":` + string(rune('0'+p)) + `}`),
			Priority: p,
		}
		require.NoError(t, d.Insert(ctx, ji))
	}

	var order []int
	doneCh := make(chan struct{})

	cfg := job.WorkerConfig{
		Executor: func(_ context.Context, _ string, payload json.RawMessage) error {
			var data struct{ P int }
			_ = json.Unmarshal(payload, &data)
			order = append(order, data.P)
			if len(order) == 3 {
				close(doneCh)
			}
			return nil
		},
		// Use concurrency=1 to ensure sequential processing for deterministic ordering.
		Queues: map[string]int{"default": 1},
	}

	require.NoError(t, d.Start(ctx, cfg))
	defer func() { _ = d.Stop(context.Background()) }()

	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for all jobs")
	}

	require.Equal(t, []int{1, 2, 3}, order)
}

func TestPollQueue_RetryOnFailure(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(50*time.Millisecond))
	ctx := context.Background()

	ji := &job.JobInsert{TaskName: "retry_task", MaxAttempts: 3}
	require.NoError(t, d.Insert(ctx, ji))

	var attempts atomic.Int32

	cfg := job.WorkerConfig{
		Executor: func(_ context.Context, _ string, _ json.RawMessage) error {
			n := attempts.Add(1)
			if n < 3 {
				return errors.New("transient error")
			}
			return nil // succeed on third attempt
		},
		Queues: map[string]int{"default": 1},
	}

	require.NoError(t, d.Start(ctx, cfg))
	defer func() { _ = d.Stop(context.Background()) }()

	require.Eventually(t, func() bool { return attempts.Load() >= 3 }, 5*time.Second, 25*time.Millisecond)

	// Give a moment for status update.
	time.Sleep(100 * time.Millisecond)

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM forge_jobs`).Scan(&status))
	require.Equal(t, "completed", status)
}

func TestPollQueue_DiscardOnMaxAttempts(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(50*time.Millisecond))
	ctx := context.Background()

	ji := &job.JobInsert{TaskName: "discard_task", MaxAttempts: 2}
	require.NoError(t, d.Insert(ctx, ji))

	var attempts atomic.Int32

	cfg := job.WorkerConfig{
		Executor: func(_ context.Context, _ string, _ json.RawMessage) error {
			attempts.Add(1)
			return errors.New("permanent error")
		},
		Queues: map[string]int{"default": 1},
	}

	require.NoError(t, d.Start(ctx, cfg))
	defer func() { _ = d.Stop(context.Background()) }()

	require.Eventually(t, func() bool { return attempts.Load() >= 2 }, 5*time.Second, 25*time.Millisecond)

	// Give a moment for status update.
	time.Sleep(100 * time.Millisecond)

	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM forge_jobs`).Scan(&status))
	require.Equal(t, "discarded", status)
}

func TestPollQueue_PanicRecovery(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(50*time.Millisecond))
	ctx := context.Background()

	ji := &job.JobInsert{TaskName: "panic_task", MaxAttempts: 1}
	require.NoError(t, d.Insert(ctx, ji))

	var called atomic.Bool

	cfg := job.WorkerConfig{
		Executor: func(_ context.Context, _ string, _ json.RawMessage) error {
			called.Store(true)
			panic("test panic")
		},
		Queues: map[string]int{"default": 1},
	}

	require.NoError(t, d.Start(ctx, cfg))
	defer func() { _ = d.Stop(context.Background()) }()

	require.Eventually(t, func() bool { return called.Load() }, 2*time.Second, 25*time.Millisecond)

	// Give a moment for status update.
	time.Sleep(100 * time.Millisecond)

	// Panic with max_attempts=1 → discarded.
	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM forge_jobs`).Scan(&status))
	require.Equal(t, "discarded", status)
}

// TestExecuteJob_PersistsResultDuringStop verifies that a job which finishes
// while Stop is cancelling the poller context still persists its terminal
// status (rather than being left in 'running' because the write used the
// cancelled context).
func TestExecuteJob_PersistsResultDuringStop(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(20*time.Millisecond))
	ctx := context.Background()

	ji := &job.JobInsert{TaskName: "slow_task"}
	require.NoError(t, d.Insert(ctx, ji))

	started := make(chan struct{})
	release := make(chan struct{})
	var observedCtxErr atomic.Bool

	cfg := job.WorkerConfig{
		Executor: func(execCtx context.Context, _ string, _ json.RawMessage) error {
			close(started)
			<-release
			// By now Stop has cancelled the poller ctx; record that so the test
			// confirms it actually raced with shutdown.
			if execCtx.Err() != nil {
				observedCtxErr.Store(true)
			}
			return nil
		},
		Queues: map[string]int{"default": 1},
	}

	require.NoError(t, d.Start(ctx, cfg))

	// Wait until the job is mid-flight.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("job never started")
	}

	// Begin Stop in the background (it blocks waiting for the in-flight job).
	stopDone := make(chan error, 1)
	go func() { stopDone <- d.Stop(context.Background()) }()

	// Give Stop a moment to cancel the poller context, then let the job finish.
	time.Sleep(100 * time.Millisecond)
	close(release)

	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not return")
	}

	require.True(t, observedCtxErr.Load(), "executor should have seen the cancelled poller context")

	// The completed status must have been persisted despite the cancellation.
	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM forge_jobs`).Scan(&status))
	require.Equal(t, "completed", status)
}

func TestPollQueue_CompletedOnSuccess(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	d := New(db, WithPollInterval(50*time.Millisecond))
	ctx := context.Background()

	ji := &job.JobInsert{TaskName: "success_task"}
	require.NoError(t, d.Insert(ctx, ji))

	var done atomic.Bool

	cfg := job.WorkerConfig{
		Executor: func(_ context.Context, _ string, _ json.RawMessage) error {
			done.Store(true)
			return nil
		},
		Queues: map[string]int{"default": 1},
	}

	require.NoError(t, d.Start(ctx, cfg))
	defer func() { _ = d.Stop(context.Background()) }()

	require.Eventually(t, func() bool { return done.Load() }, 2*time.Second, 25*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	var status string
	var completedAt sql.NullString
	require.NoError(t, db.QueryRow(`SELECT status, completed_at FROM forge_jobs`).Scan(&status, &completedAt))
	require.Equal(t, "completed", status)
	require.True(t, completedAt.Valid)
}
