package quota_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/quota"
)

func TestCalendarMonthly(t *testing.T) {
	w := quota.Calendar(quota.Monthly, nil) // nil => UTC
	now := time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC)
	period, reset := w("tenant", now)
	assert.Equal(t, "2026-07", period)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), reset)
}

func TestGaugeHasNoPeriodOrReset(t *testing.T) {
	period, reset := quota.Gauge()("s", time.Now())
	assert.Equal(t, "", period)
	assert.True(t, reset.IsZero())
}

func TestLimitValidate(t *testing.T) {
	assert.NoError(t, quota.Limit{Included: 10, Max: 10}.Validate())
	assert.NoError(t, quota.Limit{Included: 10, Max: quota.Unlimited}.Validate())
	assert.ErrorIs(t, quota.Limit{Included: 10, Max: 5}.Validate(), quota.ErrInvalidLimit)
}
