package singleflight_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/singleflight"
)

func TestCoalescesConcurrentCalls(t *testing.T) {
	var g singleflight.Group[int]
	var calls atomic.Int32
	start := make(chan struct{})
	results := make([]int, 20)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, _, err := g.Do(t.Context(), "k", func(context.Context) (int, error) {
				calls.Add(1)
				time.Sleep(25 * time.Millisecond)
				return 42, nil
			})
			assert.NoError(t, err)
			results[i] = v
		}()
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
