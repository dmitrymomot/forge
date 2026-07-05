package supervisor_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/supervisor"
)

// The hook must FINISH before the service observes ctx cancellation.
func TestWithPreShutdown_RunsBeforeServiceCancel(t *testing.T) {
	var mu atomic.Int32 // sequence counter

	hookSeq := int32(-1)
	svcSeq := int32(-1)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()

	err := supervisor.Run(ctx,
		supervisor.WithPreShutdown("drain", func(context.Context) {
			hookSeq = mu.Add(1)
		}),
		supervisor.WithServiceFunc("svc", func(sctx context.Context) error {
			<-sctx.Done()
			svcSeq = mu.Add(1)
			return nil
		}),
		supervisor.WithShutdownTimeout(time.Second),
	)
	require.NoError(t, err)
	assert.Equal(t, int32(1), hookSeq, "hook should run first")
	assert.Equal(t, int32(2), svcSeq, "service cancel should observe after hook")
}

func TestWithPreShutdown_TimeoutSurfaces(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()

	err := supervisor.Run(ctx,
		supervisor.WithPreShutdownTimeout(20*time.Millisecond),
		supervisor.WithPreShutdown("slow", func(hctx context.Context) {
			<-hctx.Done() // never returns before the bound
		}),
		supervisor.WithServiceFunc("svc", func(sctx context.Context) error { <-sctx.Done(); return nil }),
		supervisor.WithShutdownTimeout(time.Second),
	)
	assert.ErrorIs(t, err, supervisor.ErrPreShutdownTimeout)
}

func TestWithPreShutdown_NilFuncRejected(t *testing.T) {
	err := supervisor.Run(context.Background(),
		supervisor.WithPreShutdown("bad", nil),
		supervisor.WithServiceFunc("svc", func(context.Context) error { return nil }),
	)
	assert.ErrorIs(t, err, supervisor.ErrInvalidConfig)
}
