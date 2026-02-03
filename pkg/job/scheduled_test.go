package job

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduledTaskExecutor_Execute(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		called := false
		handler := func(ctx context.Context) error {
			called = true
			return nil
		}

		executor := &scheduledTaskExecutor{handler: handler}
		err := executor.Execute(context.Background(), nil)

		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("handler error", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("handler failed")
		handler := func(ctx context.Context) error {
			return expectedErr
		}

		executor := &scheduledTaskExecutor{handler: handler}
		err := executor.Execute(context.Background(), nil)

		require.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("ignores payload", func(t *testing.T) {
		t.Parallel()

		called := false
		handler := func(ctx context.Context) error {
			called = true
			return nil
		}

		executor := &scheduledTaskExecutor{handler: handler}
		// Pass payload that should be ignored
		err := executor.Execute(context.Background(), []byte(`{"ignored":"data"}`))

		require.NoError(t, err)
		assert.True(t, called)
	})
}
