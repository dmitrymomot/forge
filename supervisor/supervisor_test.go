package supervisor

import (
	"bytes"
	"context"
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
	assert.Contains(t, err.Error(), `service "svc"`)
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
	default:
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
