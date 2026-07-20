package scheduler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/scheduler"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestCronParseErrors(t *testing.T) {
	t.Parallel()

	specs := []string{
		"",
		"* * * *",
		"* * * * * *",
		"60 * * * *",
		"* 24 * * *",
		"* * 0 * *",
		"* * 32 * *",
		"* * * 13 *",
		"* * * 0 *",
		"* * * * 8",
		"*/0 * * * *",
		"*/-1 * * * *",
		"a * * * *",
		"5-1 * * * *",
		"1--2 * * * *",
		"1/2/3 * * * *",
		"* * * * mon-sun",
		"@nope",
		"0 8 * * * ; DROP",
	}
	for _, spec := range specs {
		_, err := scheduler.Cron(spec)
		require.ErrorIs(t, err, scheduler.ErrInvalidSpec, "spec %q", spec)
	}
}

func TestCronNext(t *testing.T) {
	t.Parallel()

	cases := []struct {
		spec string
		from string
		want string
	}{
		// Every minute: strictly after, seconds truncated.
		{"* * * * *", "2026-01-01T00:00:30Z", "2026-01-01T00:01:00Z"},
		{"* * * * *", "2026-01-01T00:01:00Z", "2026-01-01T00:02:00Z"},
		// Daily at 08:00.
		{"0 8 * * *", "2026-07-20T07:59:00Z", "2026-07-20T08:00:00Z"},
		{"0 8 * * *", "2026-07-20T08:00:00Z", "2026-07-21T08:00:00Z"},
		// Weekday range with names; 2026-07-18 is a Saturday.
		{"30 14 * * mon-fri", "2026-07-18T00:00:00Z", "2026-07-20T14:30:00Z"},
		// Steps.
		{"*/15 * * * *", "2026-01-01T10:07:00Z", "2026-01-01T10:15:00Z"},
		{"5/20 * * * *", "2026-01-01T10:26:00Z", "2026-01-01T10:45:00Z"},
		{"10-30/10 * * * *", "2026-01-01T10:21:00Z", "2026-01-01T10:30:00Z"},
		// Lists and month names.
		{"0 0 1,15 * *", "2026-01-02T00:00:00Z", "2026-01-15T00:00:00Z"},
		{"0 0 1 jan *", "2026-03-01T00:00:00Z", "2027-01-01T00:00:00Z"},
		// Sunday as 0 and as 7; 2026-07-26 is a Sunday.
		{"0 0 * * 0", "2026-07-20T00:00:00Z", "2026-07-26T00:00:00Z"},
		{"0 0 * * 7", "2026-07-20T00:00:00Z", "2026-07-26T00:00:00Z"},
		{"0 0 * * fri-7", "2026-07-20T00:00:00Z", "2026-07-24T00:00:00Z"},
		// Vixie OR: both day fields restricted — 13th OR Friday, whichever first.
		{"0 0 13 * fri", "2026-07-20T00:00:00Z", "2026-07-24T00:00:00Z"},
		// Restricted day-of-month with * day-of-week: only the 13th.
		{"0 0 13 * *", "2026-07-20T00:00:00Z", "2026-08-13T00:00:00Z"},
		// Leap day: from 2026, next Feb 29 is 2028.
		{"0 0 29 2 *", "2026-03-01T00:00:00Z", "2028-02-29T00:00:00Z"},
		// Aliases.
		{"@daily", "2026-07-20T13:45:00Z", "2026-07-21T00:00:00Z"},
		{"@hourly", "2026-07-20T13:45:00Z", "2026-07-20T14:00:00Z"},
		{"@weekly", "2026-07-20T13:45:00Z", "2026-07-26T00:00:00Z"},
		{"@monthly", "2026-07-20T13:45:00Z", "2026-08-01T00:00:00Z"},
		{"@yearly", "2026-07-20T13:45:00Z", "2027-01-01T00:00:00Z"},
	}
	for _, tc := range cases {
		sched, err := scheduler.Cron(tc.spec)
		require.NoError(t, err, "spec %q", tc.spec)
		got := sched.Next(at(tc.from))
		assert.Equal(t, at(tc.want), got.UTC(), "spec %q from %s", tc.spec, tc.from)
	}
}

func TestCronVixieOrRule(t *testing.T) {
	t.Parallel()

	sched, err := scheduler.Cron("0 0 13 * fri")
	require.NoError(t, err)
	// 2026-07-10 is a Friday; from the 10th the next fire is Monday the 13th
	// (day-of-month), then Friday the 17th (day-of-week).
	first := sched.Next(at("2026-07-10T00:00:00Z"))
	assert.Equal(t, at("2026-07-13T00:00:00Z"), first.UTC())
	assert.Equal(t, at("2026-07-17T00:00:00Z"), sched.Next(first).UTC())
}

func TestCronUnsatisfiable(t *testing.T) {
	t.Parallel()

	sched, err := scheduler.Cron("0 0 31 2 *")
	require.NoError(t, err)
	assert.True(t, sched.Next(at("2026-01-01T00:00:00Z")).IsZero())
}

func TestCronSequence(t *testing.T) {
	t.Parallel()

	sched, err := scheduler.Cron("*/20 9-10 * * *")
	require.NoError(t, err)
	got := make([]time.Time, 0, 7)
	next := at("2026-07-20T08:00:00Z")
	for range 7 {
		next = sched.Next(next)
		got = append(got, next.UTC())
	}
	want := []time.Time{
		at("2026-07-20T09:00:00Z"),
		at("2026-07-20T09:20:00Z"),
		at("2026-07-20T09:40:00Z"),
		at("2026-07-20T10:00:00Z"),
		at("2026-07-20T10:20:00Z"),
		at("2026-07-20T10:40:00Z"),
		at("2026-07-21T09:00:00Z"),
	}
	assert.Equal(t, want, got)
}

func TestCronIn(t *testing.T) {
	t.Parallel()

	t.Run("nil location", func(t *testing.T) {
		t.Parallel()
		_, err := scheduler.CronIn("* * * * *", nil)
		require.ErrorIs(t, err, scheduler.ErrInvalidSpec)
	})

	t.Run("wall clock in zone", func(t *testing.T) {
		t.Parallel()
		loc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)
		sched, err := scheduler.CronIn("0 8 * * *", loc)
		require.NoError(t, err)
		// 2026-07-20 is EDT (UTC-4): 08:00 wall = 12:00 UTC.
		got := sched.Next(at("2026-07-20T00:00:00Z"))
		assert.Equal(t, at("2026-07-20T12:00:00Z"), got.UTC())
	})

	t.Run("spring forward gap skipped", func(t *testing.T) {
		t.Parallel()
		loc, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)
		sched, err := scheduler.CronIn("30 2 * * *", loc)
		require.NoError(t, err)
		// 2026-03-08 02:30 EST does not exist; the tick skips to 03-09.
		from := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
		got := sched.Next(from)
		assert.Equal(t, time.Date(2026, 3, 9, 2, 30, 0, 0, loc), got)
	})
}

func TestMustCron(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, scheduler.MustCron("0 8 * * *"))
	assert.Panics(t, func() { scheduler.MustCron("not a spec") })
}
