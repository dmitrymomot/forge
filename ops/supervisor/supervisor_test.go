package supervisor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/supervisor"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRun_NoServices_ReturnsErrNoServices(t *testing.T) {
	err := supervisor.Run(context.Background())
	require.ErrorIs(t, err, supervisor.ErrNoServices)
}

func TestRun_EmptyName_ReturnsErrUnnamedService(t *testing.T) {
	svc := fakeService{name: "", run: func(ctx context.Context) error { return nil }}
	err := supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))
	require.ErrorIs(t, err, supervisor.ErrUnnamedService)
}

func TestRun_SingleService_ReturnsWrappedError(t *testing.T) {
	sentinel := errors.New("boom")
	svc := fakeService{name: "svc", run: func(ctx context.Context) error { return sentinel }}

	err := supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), `service "svc"`) //nolint:nilaway // err is guaranteed non-nil by require.ErrorIs above
}

func TestRun_FirstExitStopsAll(t *testing.T) {
	siblingCancelled := make(chan struct{})
	sibling := fakeService{name: "sibling", run: func(ctx context.Context) error {
		<-ctx.Done()
		close(siblingCancelled)
		return ctx.Err()
	}}
	quick := fakeService{name: "quick", run: func(ctx context.Context) error { return nil }}

	err := supervisor.Run(context.Background(),
		supervisor.WithService(sibling), supervisor.WithService(quick), supervisor.WithLogger(discardLogger()))

	require.NoError(t, err, "quick returns nil; sibling returns context.Canceled which is filtered")
	select {
	case <-siblingCancelled:
	case <-time.After(time.Second):
		t.Fatal("sibling was not cancelled when quick exited")
	}
}

func TestRun_ContextCancel_ShutsDown(t *testing.T) {
	svc := fakeService{name: "svc", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := supervisor.Run(ctx, supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))
	require.NoError(t, err)
}

func TestRun_AggregatesNonCanceledErrors(t *testing.T) {
	errA := errors.New("err-a")
	errB := errors.New("err-b")
	a := fakeService{name: "a", run: func(ctx context.Context) error { return errA }}
	b := fakeService{name: "b", run: func(ctx context.Context) error {
		<-ctx.Done()
		return errB // a real error during drain, NOT context.Canceled
	}}

	err := supervisor.Run(context.Background(), supervisor.WithService(a), supervisor.WithService(b), supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, errA)
	require.ErrorIs(t, err, errB)
}

func TestRun_AlreadyCancelledContext_DoesNotStartServices(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := false
	svc := fakeService{name: "svc", run: func(ctx context.Context) error {
		started = true
		return nil
	}}

	err := supervisor.Run(ctx, supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))

	require.NoError(t, err)
	assert.False(t, started, "no service may start when ctx is already cancelled")
}

func TestRun_DuplicateNames_Warns(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	a := fakeService{name: "dup", run: func(ctx context.Context) error { return nil }}
	b := fakeService{name: "dup", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}

	_ = supervisor.Run(context.Background(), supervisor.WithService(a), supervisor.WithService(b), supervisor.WithLogger(logger))

	assert.Contains(t, buf.String(), "duplicate service name")
}

func TestRun_NilLogger_DoesNotPanic(t *testing.T) {
	svc := fakeService{name: "svc", run: func(ctx context.Context) error { return nil }}
	require.NotPanics(t, func() {
		err := supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(nil))
		require.NoError(t, err)
	})
}

func TestRun_GraceTimeout_AbandonsStuckService(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	stuck := fakeService{name: "stuck", run: func(ctx context.Context) error {
		<-release // deliberately ignores ctx
		return nil
	}}
	trigger := fakeService{name: "trigger", run: func(ctx context.Context) error {
		return nil // exits immediately -> begins shutdown
	}}

	start := time.Now()
	err := supervisor.Run(context.Background(),
		supervisor.WithService(stuck), supervisor.WithService(trigger),
		supervisor.WithShutdownTimeout(50*time.Millisecond),
		supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, supervisor.ErrShutdownTimeout)
	assert.Less(t, time.Since(start), 2*time.Second, "must return shortly after the grace timeout")
}

func TestRun_GraceTimeout_LogsStuckNamesStructured(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	stuck := fakeService{name: "stuck-svc", run: func(ctx context.Context) error { <-release; return nil }}
	trigger := fakeService{name: "trigger", run: func(ctx context.Context) error { return nil }}

	_ = supervisor.Run(context.Background(),
		supervisor.WithService(stuck), supervisor.WithService(trigger),
		supervisor.WithShutdownTimeout(30*time.Millisecond), supervisor.WithLogger(logger))

	var timeoutRec map[string]any
	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] == "graceful shutdown timed out" {
			timeoutRec = rec
			break
		}
	}
	require.NotNil(t, timeoutRec, "expected a 'graceful shutdown timed out' log line")
	assert.Contains(t, timeoutRec["stuck"], "stuck-svc", "stuck name must be in the structured 'stuck' attribute")
}

func TestRun_ZeroTimeout_DrainsCooperativeService(t *testing.T) {
	svc := fakeService{name: "coop", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := supervisor.Run(ctx, supervisor.WithService(svc), supervisor.WithShutdownTimeout(0), supervisor.WithLogger(discardLogger()))
	require.NoError(t, err)
}

func TestRun_RecoverEnabled_ReturnsSingleLineErrPanic(t *testing.T) {
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}

	err := supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, supervisor.ErrPanic)
	assert.Contains(t, err.Error(), "kaboom")                                    //nolint:nilaway // err is guaranteed non-nil by require.ErrorIs above
	assert.NotContains(t, err.Error(), "\n", "error string must be single-line") //nolint:nilaway // err is guaranteed non-nil by require.ErrorIs above
}

func TestRun_RecoverEnabled_LogsStackAsAttribute(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}

	_ = supervisor.Run(context.Background(), supervisor.WithService(svc), supervisor.WithLogger(logger))

	var panicRec map[string]any
	for line := range bytes.SplitSeq(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		if rec["msg"] == "service panicked" {
			panicRec = rec
			break
		}
	}
	require.NotNil(t, panicRec, "expected a 'service panicked' log line")
	assert.Equal(t, "boom", panicRec["service"])
	stack, ok := panicRec["stack"].(string)
	require.True(t, ok, "stack must be a structured string attribute")
	assert.Contains(t, stack, "goroutine")
}

// TestRun_RecoverDisabled_PanicCrashesProcess is the black-box replacement for the
// former white-box recover-disabled test. With recovery off, an unrecovered panic in
// a service goroutine crashes the whole process, so it cannot be observed in-process.
// The test re-execs itself: the gated child runs Run(WithRecover(false)) with a
// panicking service and crashes; the parent asserts the child exited non-zero with
// the panic message in its output.
func TestRun_RecoverDisabled_PanicCrashesProcess(t *testing.T) {
	if os.Getenv("GO_SUPERVISOR_CRASH_CHILD") == "1" {
		_ = supervisor.Run(context.Background(),
			supervisor.WithServiceFunc("boom", func(context.Context) error { panic("kaboom") }),
			supervisor.WithRecover(false),
			supervisor.WithLogger(discardLogger()))
		return // guard: if recovery were (incorrectly) enabled, Run would return here; without this the child would fall through to the re-exec branch below and fork-bomb
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRun_RecoverDisabled_PanicCrashesProcess$")
	cmd.Env = append(os.Environ(), "GO_SUPERVISOR_CRASH_CHILD=1")
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "child must exit non-zero from the unrecovered panic")
	assert.Contains(t, string(out), "panic: kaboom")
}

func TestRun_InvalidConfigReturnsError(t *testing.T) {
	ok := fakeService{name: "ok", run: func(ctx context.Context) error { return nil }}
	err := supervisor.Run(context.Background(),
		supervisor.WithService(ok),
		supervisor.WithService(nil),
		supervisor.WithShutdownTimeout(-1),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, supervisor.ErrInvalidConfig)
}

// TestRun_NilRegistration_ReturnsErrInvalidConfig absorbs the former
// TestWithService_NilAppendsError and TestWithServiceFunc_NilFuncAppendsError.
func TestRun_NilRegistration_ReturnsErrInvalidConfig(t *testing.T) {
	cases := map[string]supervisor.Option{
		"nil service": supervisor.WithService(nil),
		"nil func":    supervisor.WithServiceFunc("w", nil),
	}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			err := supervisor.Run(context.Background(), opt, supervisor.WithLogger(discardLogger()))
			require.Error(t, err)
			assert.ErrorIs(t, err, supervisor.ErrInvalidConfig)
			assert.NotErrorIs(t, err, supervisor.ErrNoServices, "invalid-config must short-circuit before the no-services check")
		})
	}
}

// TestRun_WithConfigAppliesBlock absorbs the former TestWithConfig_SetsWholeBlock:
// the grace timeout from the WithConfig block must take effect.
func TestRun_WithConfigAppliesBlock(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stuck := fakeService{name: "stuck", run: func(ctx context.Context) error { <-release; return nil }}
	trigger := fakeService{name: "trigger", run: func(ctx context.Context) error { return nil }}

	err := supervisor.Run(context.Background(),
		supervisor.WithService(stuck), supervisor.WithService(trigger),
		supervisor.WithConfig(supervisor.Config{ShutdownTimeout: 50 * time.Millisecond, Recover: true}),
		supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, supervisor.ErrShutdownTimeout, "WithConfig must apply ShutdownTimeout from the block")
}

func TestRun_PanicTriggersGracefulShutdown(t *testing.T) {
	siblingDrained := make(chan struct{})
	sibling := fakeService{name: "sibling", run: func(ctx context.Context) error {
		<-ctx.Done()
		close(siblingDrained)
		return ctx.Err()
	}}
	panicky := fakeService{name: "panicky", run: func(ctx context.Context) error { panic("boom") }}

	err := supervisor.Run(context.Background(),
		supervisor.WithService(sibling), supervisor.WithService(panicky), supervisor.WithLogger(discardLogger()))

	require.ErrorIs(t, err, supervisor.ErrPanic)
	select {
	case <-siblingDrained:
	case <-time.After(time.Second):
		t.Fatal("sibling did not drain when the other service panicked")
	}
}
