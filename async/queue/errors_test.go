package queue_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
)

func TestSkipRetry_WrapsAndDetects(t *testing.T) {
	t.Parallel()
	base := errors.New("poison payload")
	err := queue.SkipRetry(base)
	require.Error(t, err)
	assert.True(t, queue.IsSkipRetry(err))
	assert.ErrorIs(t, err, base) // original error stays reachable
	assert.False(t, queue.IsSkipRetry(base))
	assert.False(t, queue.IsSkipRetry(nil))
}

func TestSkipRetry_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, queue.SkipRetry(nil))
}

func TestCancel_IsSentinel(t *testing.T) {
	t.Parallel()
	assert.True(t, errors.Is(queue.Cancel, queue.Cancel))
	wrapped := errors.Join(queue.Cancel, errors.New("context"))
	assert.True(t, errors.Is(wrapped, queue.Cancel))
	assert.False(t, errors.Is(errors.New("x"), queue.Cancel))
}
