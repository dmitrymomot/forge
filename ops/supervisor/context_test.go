package supervisor_test

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/supervisor"
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

func TestNewContext_ForceQuit_FirstSignalCancels(t *testing.T) {
	ctx, stop := supervisor.NewContext(supervisor.WithForceQuit())
	defer stop()
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	select {
	case <-ctx.Done():
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("context not cancelled after first signal")
	}
}

func TestNewContext_ForceQuit_SecondSignalExits(t *testing.T) {
	if os.Getenv("FORGE_FORCEQUIT_CHILD") == "1" {
		_, stop := supervisor.NewContext(supervisor.WithForceQuit())
		defer stop()
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
		time.Sleep(2 * time.Second) // parent expects exit(130) before this returns
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestNewContext_ForceQuit_SecondSignalExits")
	cmd.Env = append(os.Environ(), "FORGE_FORCEQUIT_CHILD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 130, ee.ExitCode())
}
