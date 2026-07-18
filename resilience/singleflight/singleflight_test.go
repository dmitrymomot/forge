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

func TestMixedDoAndDoDetachedCoalesce(t *testing.T) {
	var g singleflight.Group[int]
	var calls atomic.Int32

	// A DoDetached-led flight is joined by concurrent Do callers.
	entered := make(chan struct{})
	var leaderShared bool
	var wg sync.WaitGroup
	wg.Go(func() {
		v, shared, err := g.DoDetached(t.Context(), "k", func(context.Context) (int, error) {
			calls.Add(1)
			close(entered)
			time.Sleep(25 * time.Millisecond)
			return 42, nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 42, v)
		leaderShared = shared
	})
	<-entered // the DoDetached flight is registered and running
	joined := make([]bool, 10)
	vals := make([]int, 10)
	for i := range joined {
		wg.Go(func() {
			v, shared, err := g.Do(t.Context(), "k", func(context.Context) (int, error) {
				calls.Add(1)
				return 0, nil
			})
			assert.NoError(t, err)
			joined[i] = shared
			vals[i] = v
		})
	}
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load(), "mixed callers must share one execution")
	assert.False(t, leaderShared, "the DoDetached initiator led the flight")
	for i := range joined {
		assert.True(t, joined[i], "Do caller %d must join the in-flight execution", i)
		assert.Equal(t, 42, vals[i])
	}

	// Vice versa: a Do-led flight is joined by a DoDetached caller.
	calls.Store(0)
	entered = make(chan struct{})
	var wg2 sync.WaitGroup
	var joinerShared bool
	var joinerVal int
	wg2.Go(func() {
		<-entered
		v, shared, err := g.DoDetached(t.Context(), "k", func(context.Context) (int, error) {
			calls.Add(1)
			return 0, nil
		})
		assert.NoError(t, err)
		joinerShared = shared
		joinerVal = v
	})
	v, shared, err := g.Do(t.Context(), "k", func(context.Context) (int, error) {
		calls.Add(1)
		close(entered)
		time.Sleep(25 * time.Millisecond)
		return 7, nil
	})
	wg2.Wait()
	assert.NoError(t, err)
	assert.False(t, shared, "the Do leader ran fn itself")
	assert.Equal(t, 7, v)
	assert.Equal(t, int32(1), calls.Load(), "mixed callers must share one execution")
	assert.True(t, joinerShared, "the DoDetached caller must join the Do-led flight")
	assert.Equal(t, 7, joinerVal)
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
