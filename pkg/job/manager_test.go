package job

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager_NilDriver(t *testing.T) {
	t.Parallel()

	_, err := NewManager(nil, Config{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrDriverRequired)
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
