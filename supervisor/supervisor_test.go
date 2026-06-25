package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRun_NoServices_ReturnsErrNoServices(t *testing.T) {
	err := Run(context.Background())
	require.ErrorIs(t, err, ErrNoServices)
}

func TestRun_EmptyName_ReturnsErrUnnamedService(t *testing.T) {
	svc := fakeService{name: "", run: func(ctx context.Context) error { return nil }}
	err := Run(context.Background(), WithService(svc), WithLogger(discardLogger()))
	require.ErrorIs(t, err, ErrUnnamedService)
}

func TestRun_SingleService_ReturnsWrappedError(t *testing.T) {
	sentinel := errors.New("boom")
	svc := fakeService{name: "svc", run: func(ctx context.Context) error { return sentinel }}

	err := Run(context.Background(), WithService(svc), WithLogger(discardLogger()))

	require.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), `service "svc"`) //nolint:nilaway // err is guaranteed non-nil by require.ErrorIs above; nilaway does not track require's fatal-on-nil behavior
}

func TestRun_FirstExitStopsAll(t *testing.T) {
	siblingCancelled := make(chan struct{})
	sibling := fakeService{name: "sibling", run: func(ctx context.Context) error {
		<-ctx.Done()
		close(siblingCancelled)
		return ctx.Err()
	}}
	quick := fakeService{name: "quick", run: func(ctx context.Context) error { return nil }}

	err := Run(context.Background(),
		WithService(sibling), WithService(quick), WithLogger(discardLogger()))

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

	err := Run(ctx, WithService(svc), WithLogger(discardLogger()))
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

	err := Run(context.Background(), WithService(a), WithService(b), WithLogger(discardLogger()))

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

	err := Run(ctx, WithService(svc), WithLogger(discardLogger()))

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

	_ = Run(context.Background(), WithService(a), WithService(b), WithLogger(logger))

	assert.Contains(t, buf.String(), "duplicate service name")
}

func TestResolveLogger_NilReturnsUsableLogger(t *testing.T) {
	got := resolveLogger(nil)
	require.NotNil(t, got)
	got.Info("must not panic")
}

func TestResolveLogger_PassthroughWhenSet(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	assert.Same(t, l, resolveLogger(l))
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
	err := Run(context.Background(),
		WithService(stuck), WithService(trigger),
		WithShutdownTimeout(50*time.Millisecond),
		WithLogger(discardLogger()))

	require.ErrorIs(t, err, ErrShutdownTimeout)
	assert.Less(t, time.Since(start), 2*time.Second, "must return shortly after the grace timeout")
}

func TestRun_GraceTimeout_LogsStuckNamesStructured(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	stuck := fakeService{name: "stuck-svc", run: func(ctx context.Context) error { <-release; return nil }}
	trigger := fakeService{name: "trigger", run: func(ctx context.Context) error { return nil }}

	_ = Run(context.Background(),
		WithService(stuck), WithService(trigger),
		WithShutdownTimeout(30*time.Millisecond), WithLogger(logger))

	out := buf.String()
	assert.Contains(t, out, "graceful shutdown timed out")
	assert.Contains(t, out, "stuck-svc", "stuck service name must appear in the structured log")
}

func TestRun_ZeroTimeout_DrainsCooperativeService(t *testing.T) {
	// With timeout 0 there is no abandon; a cooperative service still drains.
	svc := fakeService{name: "coop", run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := Run(ctx, WithService(svc), WithShutdownTimeout(0), WithLogger(discardLogger()))
	require.NoError(t, err)
}

func TestRunService_RecoverEnabled_ReturnsSingleLineErrPanic(t *testing.T) {
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}

	err := runService(context.Background(), svc, discardLogger(), true)

	require.ErrorIs(t, err, ErrPanic)
	assert.Contains(t, err.Error(), "kaboom")
	assert.NotContains(t, err.Error(), "\n", "error string must be single-line; no stack embedded")
}

func TestRunService_RecoverEnabled_LogsStackAsAttribute(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}

	_ = runService(context.Background(), svc, logger, true)

	var rec map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec))
	assert.Equal(t, "service panicked", rec["msg"])
	assert.Equal(t, "boom", rec["service"])
	stack, ok := rec["stack"].(string)
	require.True(t, ok, "stack must be a structured string attribute")
	assert.Contains(t, stack, "goroutine")
}

func TestRunService_RecoverDisabled_Propagates(t *testing.T) {
	svc := fakeService{name: "boom", run: func(ctx context.Context) error { panic("kaboom") }}
	require.Panics(t, func() {
		_ = runService(context.Background(), svc, discardLogger(), false)
	})
}

func TestRun_PanicTriggersGracefulShutdown(t *testing.T) {
	siblingDrained := make(chan struct{})
	sibling := fakeService{name: "sibling", run: func(ctx context.Context) error {
		<-ctx.Done()
		close(siblingDrained)
		return ctx.Err()
	}}
	panicky := fakeService{name: "panicky", run: func(ctx context.Context) error { panic("boom") }}

	err := Run(context.Background(),
		WithService(sibling), WithService(panicky), WithLogger(discardLogger()))

	require.ErrorIs(t, err, ErrPanic)
	select {
	case <-siblingDrained:
	default:
		t.Fatal("sibling did not drain when the other service panicked")
	}
}
