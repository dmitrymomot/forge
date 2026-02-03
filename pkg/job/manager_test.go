package job

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager_NilPool(t *testing.T) {
	_, err := NewManager(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool is required")
}

func TestParseCronSchedule_Valid(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		wantNext time.Time // relative to a fixed time
	}{
		{
			name: "every minute",
			expr: "* * * * *",
		},
		{
			name: "every hour",
			expr: "0 * * * *",
		},
		{
			name: "daily at midnight",
			expr: "0 0 * * *",
		},
		{
			name: "weekly on Sunday",
			expr: "0 0 * * 0",
		},
		{
			name: "every 15 minutes",
			expr: "*/15 * * * *",
		},
		{
			name: "specific time",
			expr: "30 14 * * *",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := parseCronSchedule(tt.expr)
			require.NoError(t, err)
			assert.NotNil(t, schedule)

			// Verify Next returns a future time
			now := time.Now()
			next := schedule.Next(now)
			assert.True(t, next.After(now), "next time should be in the future")
		})
	}
}

func TestParseCronSchedule_Invalid(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{name: "empty", expr: ""},
		{name: "too few fields", expr: "* * *"},
		{name: "too many fields", expr: "* * * * * *"},
		{name: "invalid minute", expr: "60 * * * *"},
		{name: "invalid hour", expr: "* 25 * * *"},
		{name: "invalid day", expr: "* * 32 * *"},
		{name: "invalid month", expr: "* * * 13 *"},
		{name: "invalid weekday", expr: "* * * * 8"},
		{name: "garbage", expr: "not a cron expression"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCronSchedule(tt.expr)
			assert.Error(t, err)
		})
	}
}

func TestCronScheduleAdapter_Next(t *testing.T) {
	schedule, err := parseCronSchedule("0 * * * *") // Every hour
	require.NoError(t, err)

	base := time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC)
	next := schedule.Next(base)

	// Should be at 11:00
	expected := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, next)

	// Next after that should be 12:00
	next2 := schedule.Next(next)
	expected2 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, expected2, next2)
}

func TestErrors(t *testing.T) {
	// Verify error messages
	assert.Contains(t, ErrNotConfigured.Error(), "not configured")
	assert.Contains(t, ErrUnknownTask.Error(), "unknown task")
	assert.Contains(t, ErrInvalidPayload.Error(), "invalid payload")
	assert.Contains(t, ErrAlreadyStarted.Error(), "already started")
	assert.Contains(t, ErrNotStarted.Error(), "not started")
}
