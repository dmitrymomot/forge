package job

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildJobInsert(t *testing.T) {
	t.Parallel()

	t.Run("nil payload", func(t *testing.T) {
		t.Parallel()

		ji, err := buildJobInsert("test", nil)
		require.NoError(t, err)
		assert.Equal(t, "test", ji.TaskName)
		assert.Empty(t, ji.Payload)
	})

	t.Run("valid payload", func(t *testing.T) {
		t.Parallel()

		payload := testPayload{Message: "hello", Count: 42}
		ji, err := buildJobInsert("test", payload)
		require.NoError(t, err)
		assert.Equal(t, "test", ji.TaskName)

		var decoded testPayload
		err = json.Unmarshal(ji.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, payload, decoded)
	})

	t.Run("with queue option", func(t *testing.T) {
		t.Parallel()

		ji, err := buildJobInsert("test", nil, WithQueue("email"))
		require.NoError(t, err)
		assert.Equal(t, "test", ji.TaskName)
		assert.Equal(t, "email", ji.Queue)
	})

	t.Run("with schedule option", func(t *testing.T) {
		t.Parallel()

		scheduledTime := time.Now().Add(time.Hour)
		ji, err := buildJobInsert("test", nil, WithScheduledAt(scheduledTime))
		require.NoError(t, err)
		assert.Equal(t, "test", ji.TaskName)
		require.NotNil(t, ji.ScheduledAt)
		assert.Equal(t, scheduledTime, *ji.ScheduledAt)
	})

	t.Run("with max attempts", func(t *testing.T) {
		t.Parallel()

		ji, err := buildJobInsert("test", nil, WithMaxAttempts(5))
		require.NoError(t, err)
		assert.Equal(t, "test", ji.TaskName)
		assert.Equal(t, 5, ji.MaxAttempts)
	})

	t.Run("with priority", func(t *testing.T) {
		t.Parallel()

		ji, err := buildJobInsert("test", nil, WithPriority(10))
		require.NoError(t, err)
		assert.Equal(t, "test", ji.TaskName)
		assert.Equal(t, 10, ji.Priority)
	})

	t.Run("with tags", func(t *testing.T) {
		t.Parallel()

		ji, err := buildJobInsert("test", nil, WithTags("tag1", "tag2"))
		require.NoError(t, err)
		assert.Equal(t, "test", ji.TaskName)
		assert.Equal(t, []string{"tag1", "tag2"}, ji.Tags)
	})

	t.Run("with unique options", func(t *testing.T) {
		t.Parallel()

		ji, err := buildJobInsert("test", nil,
			WithUniqueFor(time.Hour),
			WithUniqueKey("custom-key"),
		)
		require.NoError(t, err)
		assert.Equal(t, "test", ji.TaskName)
		assert.Equal(t, "custom-key", ji.UniqueKey)
		assert.Equal(t, time.Hour, ji.UniqueFor)
	})

	t.Run("combined options", func(t *testing.T) {
		t.Parallel()

		payload := testPayload{Message: "test", Count: 1}
		ji, err := buildJobInsert("test", payload,
			WithQueue("email"),
			WithMaxAttempts(3),
			WithPriority(5),
			WithTags("urgent", "email"),
			WithUniqueFor(time.Minute),
			WithUniqueKey("email:123"),
		)
		require.NoError(t, err)
		assert.Equal(t, "test", ji.TaskName)
		assert.Equal(t, "email:123", ji.UniqueKey)
		assert.Equal(t, "email", ji.Queue)
		assert.Equal(t, 3, ji.MaxAttempts)
		assert.Equal(t, 5, ji.Priority)
		assert.Equal(t, []string{"urgent", "email"}, ji.Tags)
		assert.Equal(t, time.Minute, ji.UniqueFor)

		var decoded testPayload
		err = json.Unmarshal(ji.Payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, payload, decoded)
	})
}
