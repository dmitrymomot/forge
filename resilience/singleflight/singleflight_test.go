package singleflight_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/singleflight"
)

func TestCoalescesConcurrentCalls(t *testing.T) {
	var g singleflight.Group[int]
	var calls atomic.Int32
	start := make(chan struct{})
	results := make([]int, 20)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			<-start
			v, _, err := g.Do(t.Context(), "k", func(context.Context) (int, error) {
				calls.Add(1)
				time.Sleep(25 * time.Millisecond)
				return 42, nil
			})
			assert.NoError(t, err)
			results[i] = v
		})
	}
	close(start)
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())
	for _, r := range results {
		assert.Equal(t, 42, r)
	}
}

func TestForgetAllowsReexecution(t *testing.T) {
	var g singleflight.Group[int]
	var calls atomic.Int32
	load := func(context.Context) (int, error) { calls.Add(1); return 1, nil }
	_, _, _ = g.Do(t.Context(), "k", load)
	g.Forget("k")
	_, _, _ = g.Do(t.Context(), "k", load)
	assert.Equal(t, int32(2), calls.Load())
}

func TestDoDetachedCoalescesConcurrentCalls(t *testing.T) {
	var g singleflight.Group[int]
	var calls atomic.Int32
	start := make(chan struct{})
	results := make([]int, 20)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			<-start
			v, _, err := g.DoDetached(t.Context(), "k", func(context.Context) (int, error) {
				calls.Add(1)
				time.Sleep(25 * time.Millisecond)
				return 42, nil
			})
			assert.NoError(t, err)
			results[i] = v
		})
	}
	close(start)
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())
	for _, r := range results {
		assert.Equal(t, 42, r)
	}
}

func TestDoDetachedWaitBoundedByCallerContext(t *testing.T) {
	var g singleflight.Group[int]
	release := make(chan struct{})
	defer close(release)

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, _, err := g.DoDetached(ctx, "k", func(context.Context) (int, error) {
		<-release
		return 1, nil
	})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDoDetachedExecutionSurvivesCallerCancel(t *testing.T) {
	var g singleflight.Group[int]
	done := make(chan struct{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := g.DoDetached(ctx, "k", func(fnCtx context.Context) (int, error) {
		defer close(done)
		// The detached fn must not observe the initiator's cancellation.
		assert.NoError(t, fnCtx.Err())
		return 1, nil
	})
	assert.ErrorIs(t, err, context.Canceled)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("detached fn did not run to completion after caller cancel")
	}
}

func TestDoDetachedPanicBecomesErrorForAllWaiters(t *testing.T) {
	var g singleflight.Group[int]
	_, _, err := g.DoDetached(t.Context(), "k", func(context.Context) (int, error) {
		panic("boom")
	})
	assert.ErrorContains(t, err, "panic in fn")
	// Key must not be poisoned: a subsequent call re-executes and succeeds.
	v, shared, err := g.DoDetached(t.Context(), "k", func(context.Context) (int, error) {
		return 7, nil
	})
	assert.NoError(t, err)
	assert.False(t, shared)
	assert.Equal(t, 7, v)
}

func TestPanicInFnRePanicsAndDoesNotPoisonKey(t *testing.T) {
	var g singleflight.Group[int]
	assert.Panics(t, func() {
		_, _, _ = g.Do(t.Context(), "k", func(context.Context) (int, error) {
			panic("boom")
		})
	})
	// Key must not be poisoned: a subsequent call re-executes and succeeds.
	v, shared, err := g.Do(t.Context(), "k", func(context.Context) (int, error) {
		return 7, nil
	})
	assert.NoError(t, err)
	assert.False(t, shared)
	assert.Equal(t, 7, v)
}
