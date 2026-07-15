package logger_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
)

func TestNewRejectsWithAsyncBufferSize(t *testing.T) {
	_, err := logger.New(logger.WithOutput(io.Discard), logger.WithAsyncBufferSize(64))
	require.Error(t, err)
	assert.ErrorIs(t, err, logger.ErrInvalidConfig)
}

func TestWithAsyncBufferSizeRejectsNonPositive(t *testing.T) {
	for _, n := range []int{0, -1} {
		_, err := logger.New(logger.WithOutput(io.Discard), logger.WithAsyncBufferSize(n))
		require.Error(t, err, "n=%d", n)
		assert.ErrorIs(t, err, logger.ErrInvalidConfig)
	}
}
