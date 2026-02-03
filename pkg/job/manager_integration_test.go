package job

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_buildJobArgs(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		registry: newTaskRegistry(),
	}

	t.Run("nil payload", func(t *testing.T) {
		t.Parallel()

		args, opts, err := manager.buildJobArgs("test", nil)
		require.NoError(t, err)
		assert.Equal(t, "test", args.TaskName)
		assert.Empty(t, args.Payload)
		assert.NotNil(t, opts)
	})

	t.Run("valid payload", func(t *testing.T) {
		t.Parallel()

		payload := testPayload{Message: "hello", Count: 42}
		args, opts, err := manager.buildJobArgs("test", payload)
		require.NoError(t, err)
		assert.Equal(t, "test", args.TaskName)

		var decoded testPayload
		err = json.Unmarshal(args.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, payload, decoded)
		assert.NotNil(t, opts)
	})

	t.Run("with queue option", func(t *testing.T) {
		t.Parallel()

		args, opts, err := manager.buildJobArgs("test", nil, InQueue("email"))
		require.NoError(t, err)
		assert.Equal(t, "test", args.TaskName)
		assert.Equal(t, "email", opts.Queue)
	})

	t.Run("with schedule option", func(t *testing.T) {
		t.Parallel()

		scheduledTime := time.Now().Add(time.Hour)
		args, opts, err := manager.buildJobArgs("test", nil, ScheduledAt(scheduledTime))
		require.NoError(t, err)
		assert.Equal(t, "test", args.TaskName)
		assert.Equal(t, scheduledTime, opts.ScheduledAt)
	})

	t.Run("with max attempts", func(t *testing.T) {
		t.Parallel()

		args, opts, err := manager.buildJobArgs("test", nil, MaxAttempts(5))
		require.NoError(t, err)
		assert.Equal(t, "test", args.TaskName)
		assert.Equal(t, 5, opts.MaxAttempts)
	})

	t.Run("with priority", func(t *testing.T) {
		t.Parallel()

		args, opts, err := manager.buildJobArgs("test", nil, Priority(10))
		require.NoError(t, err)
		assert.Equal(t, "test", args.TaskName)
		assert.Equal(t, 10, opts.Priority)
	})

	t.Run("with tags", func(t *testing.T) {
		t.Parallel()

		args, opts, err := manager.buildJobArgs("test", nil, Tags("tag1", "tag2"))
		require.NoError(t, err)
		assert.Equal(t, "test", args.TaskName)
		assert.Equal(t, []string{"tag1", "tag2"}, opts.Tags)
	})

	t.Run("with unique options", func(t *testing.T) {
		t.Parallel()

		args, opts, err := manager.buildJobArgs("test", nil,
			UniqueFor(time.Hour),
			UniqueKey("custom-key"),
		)
		require.NoError(t, err)
		assert.Equal(t, "test", args.TaskName)
		assert.Equal(t, "custom-key", args.UniqueKey)
		assert.Equal(t, time.Hour, opts.UniqueOpts.ByPeriod)
	})

	t.Run("combined options", func(t *testing.T) {
		t.Parallel()

		payload := testPayload{Message: "test", Count: 1}
		args, opts, err := manager.buildJobArgs("test", payload,
			InQueue("email"),
			MaxAttempts(3),
			Priority(5),
			Tags("urgent", "email"),
			UniqueFor(time.Minute),
			UniqueKey("email:123"),
		)
		require.NoError(t, err)
		assert.Equal(t, "test", args.TaskName)
		assert.Equal(t, "email:123", args.UniqueKey)
		assert.Equal(t, "email", opts.Queue)
		assert.Equal(t, 3, opts.MaxAttempts)
		assert.Equal(t, 5, opts.Priority)
		assert.Equal(t, []string{"urgent", "email"}, opts.Tags)
		assert.Equal(t, time.Minute, opts.UniqueOpts.ByPeriod)

		var decoded testPayload
		err = json.Unmarshal(args.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, payload, decoded)
	})
}

func TestForgeTaskArgs_Kind(t *testing.T) {
	t.Parallel()

	args := forgeTaskArgs{TaskName: "test"}
	assert.Equal(t, "forge:task", args.Kind())
}
