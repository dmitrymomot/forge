package job

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDriver is an in-process job.Driver that records inserts without a
// real backend, so Manager/Enqueuer behavior can be asserted without Docker.
type fakeDriver struct {
	inserts   []*JobInsert
	insertTxs []*JobInsert
}

func (d *fakeDriver) Migrate(context.Context) error { return nil }

func (d *fakeDriver) Insert(_ context.Context, j *JobInsert) error {
	d.inserts = append(d.inserts, j)
	return nil
}

func (d *fakeDriver) InsertTx(_ context.Context, _ any, j *JobInsert) error {
	d.insertTxs = append(d.insertTxs, j)
	return nil
}

func (d *fakeDriver) Start(context.Context, WorkerConfig) error { return nil }
func (d *fakeDriver) Stop(context.Context) error                { return nil }
func (d *fakeDriver) Healthcheck(context.Context) error         { return nil }

func TestNewManager_NilDriver(t *testing.T) {
	t.Parallel()

	_, err := NewManager(nil, Config{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrDriverRequired)
}

func TestManagerEnqueue_UnknownTask(t *testing.T) {
	t.Parallel()

	t.Run("unknown task rejected before driver insert", func(t *testing.T) {
		t.Parallel()

		driver := &fakeDriver{}
		m, err := NewManager(driver, Config{}, WithTask(&testTask{name: "known_task"}))
		require.NoError(t, err)

		err = m.Enqueue(context.Background(), "nope", testPayload{})
		require.ErrorIs(t, err, ErrUnknownTask)
		require.Empty(t, driver.inserts, "driver must not be called for unknown task")
	})

	t.Run("known task is enqueued", func(t *testing.T) {
		t.Parallel()

		driver := &fakeDriver{}
		m, err := NewManager(driver, Config{}, WithTask(&testTask{name: "known_task"}))
		require.NoError(t, err)

		err = m.Enqueue(context.Background(), "known_task", testPayload{Message: "hi"})
		require.NoError(t, err)
		require.Len(t, driver.inserts, 1)
		require.Equal(t, "known_task", driver.inserts[0].TaskName)
	})
}

// TestEnqueuerEnqueue_FrameworkPathValidates asserts the embedded *Enqueuer that
// the framework dispatches through (forge.Context.Enqueue) enforces
// ErrUnknownTask, so the validation cannot be bypassed by going through the
// embedded enqueuer directly.
func TestEnqueuerEnqueue_FrameworkPathValidates(t *testing.T) {
	t.Parallel()

	driver := &fakeDriver{}
	m, err := NewManager(driver, Config{},
		WithTask(&testTask{name: "known_task"}),
	)
	require.NoError(t, err)

	// Simulate the framework path: it holds m.Enqueuer (the embedded value),
	// not the Manager, and calls Enqueue/EnqueueTx on it.
	enq := m.Enqueuer

	t.Run("Enqueue rejects unknown task", func(t *testing.T) {
		t.Parallel()

		err := enq.Enqueue(context.Background(), "ghost", testPayload{})
		require.ErrorIs(t, err, ErrUnknownTask)
	})

	t.Run("EnqueueTx rejects unknown task", func(t *testing.T) {
		t.Parallel()

		err := enq.EnqueueTx(context.Background(), struct{}{}, "ghost", testPayload{})
		require.ErrorIs(t, err, ErrUnknownTask)
	})

	t.Run("known task passes through to driver", func(t *testing.T) {
		t.Parallel()

		known := &fakeDriver{}
		mgr, err := NewManager(known, Config{}, WithTask(&testTask{name: "ok"}))
		require.NoError(t, err)
		require.NoError(t, mgr.Enqueuer.Enqueue(context.Background(), "ok", testPayload{Message: "x"}))
		require.Len(t, known.inserts, 1)
		require.Equal(t, "ok", known.inserts[0].TaskName)
	})
}

// TestStandaloneEnqueuer_NoTaskValidation asserts a standalone Enqueuer (no
// registry, worker runs elsewhere) does not reject unknown tasks — validation
// is deferred to the worker side.
func TestStandaloneEnqueuer_NoTaskValidation(t *testing.T) {
	t.Parallel()

	driver := &fakeDriver{}
	enq, err := NewEnqueuer(driver)
	require.NoError(t, err)

	require.NoError(t, enq.Enqueue(context.Background(), "anything", testPayload{Message: "y"}))
	require.Len(t, driver.inserts, 1)
}

// TestEnqueuer_UniqueKeyForwardedWithoutUniqueFor asserts WithUniqueKey is not
// silently dropped when WithUniqueFor is absent.
func TestEnqueuer_UniqueKeyForwardedWithoutUniqueFor(t *testing.T) {
	t.Parallel()

	driver := &fakeDriver{}
	enq, err := NewEnqueuer(driver)
	require.NoError(t, err)

	require.NoError(t, enq.Enqueue(context.Background(), "task", testPayload{}, WithUniqueKey("user:1")))
	require.Len(t, driver.inserts, 1)
	require.Equal(t, "user:1", driver.inserts[0].UniqueKey)
	require.Zero(t, driver.inserts[0].UniqueFor)

	// Sanity: payload round-trips as JSON.
	var p testPayload
	require.NoError(t, json.Unmarshal(driver.inserts[0].Payload, &p))
}

func TestErrors(t *testing.T) {
	t.Parallel()

	// Verify error messages
	assert.Contains(t, ErrNotConfigured.Error(), "not configured")
	assert.Contains(t, ErrUnknownTask.Error(), "unknown task")
	assert.Contains(t, ErrInvalidPayload.Error(), "invalid payload")
	assert.Contains(t, ErrAlreadyStarted.Error(), "already started")
	assert.Contains(t, ErrNotStarted.Error(), "not started")
	assert.Contains(t, ErrDriverRequired.Error(), "driver is required")
	assert.Contains(t, ErrInvalidTx.Error(), "invalid transaction type")

	// Backward compatibility
	assert.ErrorIs(t, ErrPoolRequired, ErrDriverRequired)
}
