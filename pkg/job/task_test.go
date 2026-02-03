package job

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPayload is a test payload type.
type testPayload struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}

// testTask implements the task interface for testing.
type testTask struct {
	name     string
	executed bool
	payload  testPayload
	err      error
}

func (t *testTask) Name() string { return t.name }

func (t *testTask) Handle(ctx context.Context, p testPayload) error {
	t.executed = true
	t.payload = p
	return t.err
}

func TestTaskRegistry_RegisterAndGet(t *testing.T) {
	registry := newTaskRegistry()

	// Register a task
	task := &testTask{name: "test_task"}
	wrapper := newTaskWrapper[testPayload, *testTask](task)
	registry.register("test_task", wrapper)

	// Get the task
	executor, ok := registry.get("test_task")
	assert.True(t, ok)
	assert.NotNil(t, executor)

	// Try to get non-existent task
	executor, ok = registry.get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, executor)
}

func TestTaskRegistry_Names(t *testing.T) {
	registry := newTaskRegistry()

	// Empty registry
	names := registry.names()
	assert.Empty(t, names)

	// Add tasks
	task1 := &testTask{name: "task1"}
	task2 := &testTask{name: "task2"}
	registry.register("task1", newTaskWrapper[testPayload, *testTask](task1))
	registry.register("task2", newTaskWrapper[testPayload, *testTask](task2))

	names = registry.names()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "task1")
	assert.Contains(t, names, "task2")
}

func TestTaskWrapper_Execute(t *testing.T) {
	t.Run("successful execution", func(t *testing.T) {
		task := &testTask{name: "test_task"}
		wrapper := newTaskWrapper[testPayload, *testTask](task)

		payload := testPayload{Message: "hello", Count: 42}
		rawPayload, err := json.Marshal(payload)
		require.NoError(t, err)

		err = wrapper.Execute(context.Background(), rawPayload)
		assert.NoError(t, err)
		assert.True(t, task.executed)
		assert.Equal(t, "hello", task.payload.Message)
		assert.Equal(t, 42, task.payload.Count)
	})

	t.Run("empty payload", func(t *testing.T) {
		task := &testTask{name: "test_task"}
		wrapper := newTaskWrapper[testPayload, *testTask](task)

		err := wrapper.Execute(context.Background(), nil)
		assert.NoError(t, err)
		assert.True(t, task.executed)
		assert.Equal(t, testPayload{}, task.payload)
	})

	t.Run("invalid payload", func(t *testing.T) {
		task := &testTask{name: "test_task"}
		wrapper := newTaskWrapper[testPayload, *testTask](task)

		err := wrapper.Execute(context.Background(), []byte("invalid json"))
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrInvalidPayload))
	})

	t.Run("task returns error", func(t *testing.T) {
		taskErr := errors.New("task failed")
		task := &testTask{name: "test_task", err: taskErr}
		wrapper := newTaskWrapper[testPayload, *testTask](task)

		err := wrapper.Execute(context.Background(), nil)
		assert.ErrorIs(t, err, taskErr)
	})
}

// emptyPayloadTask uses an empty struct as payload.
type emptyPayloadTask struct {
	executed bool
}

func (t *emptyPayloadTask) Name() string { return "empty_payload" }

func (t *emptyPayloadTask) Handle(ctx context.Context, p struct{}) error {
	t.executed = true
	return nil
}

func TestTaskWrapper_EmptyPayload(t *testing.T) {
	task := &emptyPayloadTask{}
	wrapper := newTaskWrapper[struct{}, *emptyPayloadTask](task)

	err := wrapper.Execute(context.Background(), nil)
	assert.NoError(t, err)
	assert.True(t, task.executed)
}
