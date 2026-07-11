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

func newMeter(t *testing.T, w quota.Window, clk *clock.Mock) *quota.Meter {
	store := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(clk))
	t.Cleanup(func() { _ = store.Close() })
	return quota.New(store, w, quota.WithClock(clk))
}

func TestAllow_HardCapRejectsAndDoesNotBurn(t *testing.T) {
	clk := clock.NewMock(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	m := newMeter(t, quota.Calendar(quota.Monthly, nil), clk)
	ctx := context.Background()
	lim := quota.Limit{Included: 10, Max: 10} // hard cap

	res, err := m.Allow(ctx, "t1", 8, lim)
	require.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Equal(t, int64(8), res.Used)

	res, err = m.Allow(ctx, "t1", 5, lim) // would hit 13 > 10 → reject + rollback
	require.NoError(t, err)
	assert.False(t, res.Allowed)
	assert.Equal(t, int64(8), res.Used) // NOT burned
	assert.Equal(t, int64(2), res.Remaining)
}

func TestAllow_OverageAllowedAndReported(t *testing.T) {
	clk := clock.NewMock(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	m := newMeter(t, quota.Calendar(quota.Monthly, nil), clk)
	ctx := context.Background()
	lim := quota.Limit{Included: 100, Max: 200}

	res, err := m.Allow(ctx, "t1", 150, lim)
	require.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Equal(t, int64(50), res.Overage)
	assert.Equal(t, int64(0), res.Remaining)
}

func TestAllow_CalendarWindowRollsAtBoundary(t *testing.T) {
	clk := clock.NewMock(time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))
	m := newMeter(t, quota.Calendar(quota.Monthly, nil), clk)
	ctx := context.Background()
	lim := quota.Limit{Included: 10, Max: 10}

	_, err := m.Allow(ctx, "t1", 10, lim)
	require.NoError(t, err)

	clk.Advance(15 * 24 * time.Hour) // into August
	res, err := m.Usage(ctx, "t1", lim)
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.Used) // fresh window
}

func TestAllow_InvalidInputs(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	m := newMeter(t, quota.Gauge(), clk)
	_, err := m.Allow(context.Background(), "t1", -1, quota.Limit{Included: 1, Max: 1})
	assert.ErrorIs(t, err, quota.ErrInvalidCost)
	_, err = m.Allow(context.Background(), "t1", 1, quota.Limit{Included: 5, Max: 1})
	assert.ErrorIs(t, err, quota.ErrInvalidLimit)
}
