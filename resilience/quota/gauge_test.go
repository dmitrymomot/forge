package quota_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/quota"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func TestGauge_SeatsAcquireReleaseReconcile(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	store := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(clk))
	t.Cleanup(func() { _ = store.Close() })
	m := quota.New(store, quota.Gauge(), quota.WithClock(clk))
	ctx := context.Background()
	lim := quota.Limit{Included: 5, Max: 5}

	// add 3 seats, none expire even after a long advance (no-expiry gauge)
	_, err := m.Add(ctx, "tenant", 3)
	require.NoError(t, err)
	clk.Advance(10000 * time.Hour)
	res, err := m.Usage(ctx, "tenant", lim)
	require.NoError(t, err)
	assert.Equal(t, int64(3), res.Used)

	// release one
	_, err = m.Add(ctx, "tenant", -1)
	require.NoError(t, err)

	// reconcile from DB truth
	require.NoError(t, m.Set(ctx, "tenant", 4))
	res, err = m.Usage(ctx, "tenant", lim)
	require.NoError(t, err)
	assert.Equal(t, int64(4), res.Used)
	assert.Equal(t, int64(1), res.Remaining)

	require.NoError(t, m.Reset(ctx, "tenant"))
	res, err = m.Usage(ctx, "tenant", lim)
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Used)
}
