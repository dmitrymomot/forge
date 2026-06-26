package supervisor_test

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/supervisor"
)

func TestNewContext_CancelsOnSIGTERM(t *testing.T) {
	ctx, stop := supervisor.NewContext()
	defer stop()

	require.NoError(t, ctx.Err(), "context must be live before any signal")

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	select {
	case <-ctx.Done():
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled after SIGTERM")
	}
}

func TestNewContext_StopIsSafe(t *testing.T) {
	ctx, stop := supervisor.NewContext()
	stop() // releasing the handler must not panic
	assert.ErrorIs(t, ctx.Err(), context.Canceled, "stop cancels the context")
}
