package parallel_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/parallel"
)

func TestMapPreservesOrder(t *testing.T) {
	out, err := parallel.Map(t.Context(), []int{1, 2, 3, 4, 5}, 2,
		func(_ context.Context, n int) (int, error) { return n * n, nil })
	require.NoError(t, err)
	assert.Equal(t, []int{1, 4, 9, 16, 25}, out)
}

func TestForEachBoundsConcurrency(t *testing.T) {
	var inflight, peak atomic.Int32
	items := make([]int, 30)
	err := parallel.ForEach(t.Context(), items, 3, func(context.Context, int) error {
		n := inflight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inflight.Add(-1)
		return nil
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, peak.Load(), int32(3))
}

func TestForEachFailFast(t *testing.T) {
	err := parallel.ForEach(t.Context(), []int{1, 2, 3}, 2, func(_ context.Context, n int) error {
		if n == 2 {
			return errors.New("boom")
		}
		return nil
	})
	assert.Error(t, err)
}

func TestGroupCollectAll(t *testing.T) {
	g, _ := parallel.New(t.Context(), parallel.WithCollectAll())
	for _, n := range []int{1, 2, 3, 4} {
		g.Go(func(context.Context) error {
			if n%2 == 0 {
				return fmt.Errorf("even %d", n)
			}
			return nil
		})
	}
	err := g.Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "even 2")
	assert.Contains(t, err.Error(), "even 4")
}

func TestContextCancelledAfterWait(t *testing.T) {
	g, ctx := parallel.New(t.Context())
	g.Go(func(context.Context) error { return nil })
	require.NoError(t, g.Wait())
	// The derived context is always cancelled once Wait returns, even on success.
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}
