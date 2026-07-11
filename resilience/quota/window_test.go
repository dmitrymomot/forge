package quota_test

import (
	"strconv"
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

func TestCalendarDaily(t *testing.T) {
	w := quota.Calendar(quota.Daily, nil) // nil => UTC
	now := time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC)
	period, reset := w("tenant", now)
	assert.Equal(t, "2026-07-15", period)
	assert.Equal(t, time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC), reset)
}

func TestCalendarWeekly(t *testing.T) {
	w := quota.Calendar(quota.Weekly, nil) // nil => UTC

	tests := []struct {
		name       string
		now        time.Time
		wantPeriod string
		wantReset  time.Time
	}{
		{
			name:       "mid-week Wednesday",
			now:        time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC),
			wantPeriod: "2026-W29",
			wantReset:  time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "year boundary: Dec date in next ISO year",
			now:        time.Date(2024, 12, 31, 12, 0, 0, 0, time.UTC),
			wantPeriod: "2025-W01",
			wantReset:  time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "year boundary: Sunday belongs to prior ISO year",
			now:        time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			wantPeriod: "2022-W52",
			wantReset:  time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			period, reset := w("tenant", tt.now)
			assert.Equal(t, tt.wantPeriod, period)
			assert.Equal(t, tt.wantReset, reset)
		})
	}
}

func TestRolling(t *testing.T) {
	w := quota.Rolling(time.Hour)
	now := time.Date(2026, 7, 15, 9, 37, 12, 0, time.UTC)
	// Truncate is relative to the zero instant, so 09:37:12 floors to the 09:00 bucket.
	bucket := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	period, reset := w("tenant", now)
	assert.Equal(t, strconv.FormatInt(bucket.Unix(), 10), period)
	assert.Equal(t, bucket.Add(time.Hour), reset) // 10:00
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
