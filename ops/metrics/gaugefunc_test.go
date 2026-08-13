package metrics_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/metrics"
)

var errSourceDown = errors.New("source down")

func TestGaugeFuncReadsOnEveryRender(t *testing.T) {
	rec, snap := newRecorder(t)
	var calls atomic.Int64
	rec.GaugeFunc("queue_depth", "Jobs waiting.", func(context.Context) (float64, error) {
		return float64(calls.Add(1)), nil
	})

	assert.InDelta(t, 1.0, snap()["queue_depth"], 1e-9)
	assert.InDelta(t, 2.0, snap()["queue_depth"], 1e-9)
	assert.InDelta(t, 3.0, snap()["queue_depth"], 1e-9)
}

// TestGaugeFuncNotCalledBeforeRender is the point of the pull model: nothing runs
// until something collects, so registration itself never touches the source.
func TestGaugeFuncNotCalledBeforeRender(t *testing.T) {
	rec, snap := newRecorder(t)
	called := false
	rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) {
		called = true
		return 1, nil
	})

	assert.False(t, called)
	snap()
	assert.True(t, called)
}

func TestGaugeFuncFailureRendersNullAndCounts(t *testing.T) {
	rec, snap := newRecorder(t)
	rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) {
		return 0, errSourceDown
	})

	first := snap()
	assert.Nil(t, first["rows"])

	failures, ok := snap()[metrics.CollectFailuresMetric].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 1.0, failures[`gauge="rows"`], 1e-9)
}

func TestGaugeFuncNonFiniteCountsAsFailure(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, snap := newRecorder(t)
			rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) {
				return tt.value, nil
			})

			assert.Nil(t, snap()["rows"])
			failures, ok := snap()[metrics.CollectFailuresMetric].(map[string]any)
			require.True(t, ok)
			assert.Positive(t, failures[`gauge="rows"`])
		})
	}
}

func TestGaugeFuncContextCarriesTheCollectTimeout(t *testing.T) {
	rec, snap := newRecorder(t, metrics.WithCollectTimeout(50*time.Millisecond))
	var gotDeadline bool
	var remaining time.Duration
	rec.GaugeFunc("rows", "Rows.", func(ctx context.Context) (float64, error) {
		deadline, ok := ctx.Deadline()
		gotDeadline = ok
		remaining = time.Until(deadline)
		return 1, nil
	})

	snap()
	assert.True(t, gotDeadline)
	assert.Positive(t, remaining)
	assert.LessOrEqual(t, remaining, 50*time.Millisecond)
}

func TestGaugeFuncStalledReadIsBoundedAndCounted(t *testing.T) {
	rec, snap := newRecorder(t, metrics.WithCollectTimeout(20*time.Millisecond))
	rec.GaugeFunc("rows", "Rows.", func(ctx context.Context) (float64, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})

	start := time.Now()
	assert.Nil(t, snap()["rows"])
	assert.Less(t, time.Since(start), time.Second)

	failures, ok := snap()[metrics.CollectFailuresMetric].(map[string]any)
	require.True(t, ok)
	assert.Positive(t, failures[`gauge="rows"`])
}

func TestGaugeFuncDefaultCollectTimeout(t *testing.T) {
	rec, snap := newRecorder(t)
	var remaining time.Duration
	rec.GaugeFunc("rows", "Rows.", func(ctx context.Context) (float64, error) {
		deadline, _ := ctx.Deadline()
		remaining = time.Until(deadline)
		return 1, nil
	})

	snap()
	assert.LessOrEqual(t, remaining, metrics.DefaultCollectTimeout)
	assert.Greater(t, remaining, metrics.DefaultCollectTimeout/2)
}

func TestGaugeFuncNilPanics(t *testing.T) {
	rec, _ := newRecorder(t)
	assert.Panics(t, func() { rec.GaugeFunc("rows", "Rows.", nil) })
}

func TestGaugeFuncKindMismatchPanics(t *testing.T) {
	rec, _ := newRecorder(t)
	rec.Gauge("rows", "Rows.")
	assert.Panics(t, func() {
		rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) { return 1, nil })
	})
}

func TestGaugeFuncReregistrationKeepsTheFirstFunc(t *testing.T) {
	rec, snap := newRecorder(t)
	rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) { return 1, nil })
	rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) { return 2, nil })

	assert.InDelta(t, 1.0, snap()["rows"], 1e-9)
}

func TestGaugeFuncConcurrentRenders(t *testing.T) {
	rec, snap := newRecorder(t)
	rec.GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) { return 7, nil })

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				snap()
			}
		})
	}
	wg.Wait()

	assert.InDelta(t, 7.0, snap()["rows"], 1e-9)
}

func TestNoopGaugeFuncNeverCalls(t *testing.T) {
	called := false
	metrics.NewNoop().GaugeFunc("rows", "Rows.", func(context.Context) (float64, error) {
		called = true
		return 1, nil
	})
	assert.False(t, called)
}

func TestResolveCollectTimeout(t *testing.T) {
	assert.Equal(t, metrics.DefaultCollectTimeout, metrics.ResolveCollectTimeout(0))
	assert.Equal(t, metrics.DefaultCollectTimeout, metrics.ResolveCollectTimeout(-time.Second))
	assert.Equal(t, time.Second, metrics.ResolveCollectTimeout(time.Second))
}
