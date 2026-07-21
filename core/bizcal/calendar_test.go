package bizcal_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

func TestNew_WorkdaysCalendar(t *testing.T) {
	loc := time.UTC
	cal, err := bizcal.New(loc,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
	)
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if cal.Location() != loc {
		t.Fatalf("Location() = %v, want %v", cal.Location(), loc)
	}
}

func TestNew_WeeklyWindowsCalendar(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	windows := bizcal.MustWindows("09:00-13:00", "14:00-18:00")
	cal, err := bizcal.New(loc,
		bizcal.WithWeekday(time.Monday, windows...),
		bizcal.WithWeekday(time.Tuesday, windows...),
		bizcal.WithWeekday(time.Friday, bizcal.MustWindows("09:00-15:00")...),
	)
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if cal.Location() != loc {
		t.Fatalf("Location() = %v, want %v", cal.Location(), loc)
	}
}

func TestNew_AlwaysOpen(t *testing.T) {
	cal, err := bizcal.New(time.UTC, bizcal.WithAlwaysOpen())
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if cal == nil {
		t.Fatal("New() returned nil calendar with nil error")
	}
}

func TestNew_ShiftsOnly(t *testing.T) {
	start := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.July, 21, 17, 0, 0, 0, time.UTC)
	cal, err := bizcal.New(time.UTC, bizcal.WithShifts(bizcal.Shift(start, end)))
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if cal == nil {
		t.Fatal("New() returned nil calendar with nil error")
	}
}

func TestNew_EmptyCalendar_ErrNeverOpen(t *testing.T) {
	_, err := bizcal.New(time.UTC)
	if !errors.Is(err, bizcal.ErrNeverOpen) {
		t.Fatalf("New() = %v, want ErrNeverOpen", err)
	}
}

func TestNew_WeekdayNoWindows_ErrNeverOpen(t *testing.T) {
	// A weekday base with no windows and nothing else opens no time: the
	// calendar is structurally never open and must be rejected at New.
	_, err := bizcal.New(time.UTC, bizcal.WithWeekday(time.Monday))
	if !errors.Is(err, bizcal.ErrNeverOpen) {
		t.Fatalf("New() = %v, want ErrNeverOpen", err)
	}
}

func TestNew_WeekdayNoWindows_WithShifts_Valid(t *testing.T) {
	// A windows-empty weekday base combined with shifts is still open: the
	// shifts supply the open time, so construction must succeed.
	start := time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.July, 20, 17, 0, 0, 0, time.UTC)
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWeekday(time.Monday),
		bizcal.WithShifts(bizcal.Shift(start, end)),
	)
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if !cal.IsWorkingDay(bizcal.MustDate(2026, time.July, 20)) {
		t.Fatal("IsWorkingDay(shift day) = false, want true")
	}
}

func TestNew_WithWeekday_SameWeekdayAppends(t *testing.T) {
	// Repeated WithWeekday for the same weekday appends windows rather than
	// replacing; overlapping windows merge. A Monday configured with a
	// morning window in one call and an overlapping-into-afternoon window in
	// another must be open across the union.
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWeekday(time.Monday, bizcal.MustWindows("09:00-13:00")...),
		bizcal.WithWeekday(time.Monday, bizcal.MustWindows("12:00-18:00")...),
	)
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	monday := bizcal.MustDate(2026, time.July, 20) // a Monday
	if got, want := cal.DayDuration(monday), 9*time.Hour; got != want {
		t.Fatalf("DayDuration(Monday) = %s, want %s (merged 09-18)", got, want)
	}
	// A point inside each original window is open; the merge spans both.
	for _, hm := range []struct{ h, m int }{{10, 0}, {12, 30}, {17, 0}} {
		at := time.Date(2026, time.July, 20, hm.h, hm.m, 0, 0, time.UTC)
		if !cal.IsOpen(at) {
			t.Fatalf("IsOpen(%02d:%02d) = false, want true", hm.h, hm.m)
		}
	}
}

func TestNew_NilLocation_ErrNilLocation(t *testing.T) {
	_, err := bizcal.New(nil, bizcal.WithAlwaysOpen())
	if !errors.Is(err, bizcal.ErrNilLocation) {
		t.Fatalf("New() = %v, want ErrNilLocation", err)
	}
}

func TestNew_TwoBaseSources_Error(t *testing.T) {
	_, err := bizcal.New(time.UTC,
		bizcal.WithWorkdays(8*time.Hour, time.Monday),
		bizcal.WithWeekday(time.Tuesday, bizcal.MustWindows("09:00-17:00")...),
	)
	if err == nil {
		t.Fatal("New() = nil, want a base-source-conflict error")
	}
	if errors.Is(err, bizcal.ErrNeverOpen) || errors.Is(err, bizcal.ErrInvalidRule) {
		t.Fatalf("New() = %v, want a distinct non-sentinel error", err)
	}
}

func TestNew_TwoBaseSources_AlwaysOpenAndWorkdays_Error(t *testing.T) {
	_, err := bizcal.New(time.UTC,
		bizcal.WithAlwaysOpen(),
		bizcal.WithWorkdays(8*time.Hour, time.Monday),
	)
	if err == nil {
		t.Fatal("New() = nil, want a base-source-conflict error")
	}
}

func TestNew_WithWeekday_InvalidWeekday(t *testing.T) {
	_, err := bizcal.New(time.UTC, bizcal.WithWeekday(time.Weekday(9)))
	if !errors.Is(err, bizcal.ErrInvalidWeekday) {
		t.Fatalf("New() = %v, want ErrInvalidWeekday", err)
	}
}

func TestNew_WithWorkdays_InvalidCapacity(t *testing.T) {
	cases := []struct {
		name   string
		perDay time.Duration
	}{
		{"zero", 0},
		{"negative", -time.Hour},
		{"over 24h", 25 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := bizcal.New(time.UTC, bizcal.WithWorkdays(c.perDay, time.Monday))
			if !errors.Is(err, bizcal.ErrInvalidCapacity) {
				t.Fatalf("New() = %v, want ErrInvalidCapacity", err)
			}
		})
	}
}

func TestNew_WithWorkdays_InvalidWeekday(t *testing.T) {
	_, err := bizcal.New(time.UTC, bizcal.WithWorkdays(8*time.Hour, time.Weekday(-1)))
	if !errors.Is(err, bizcal.ErrInvalidWeekday) {
		t.Fatalf("New() = %v, want ErrInvalidWeekday", err)
	}
}

func TestNew_WithRule_Nil(t *testing.T) {
	_, err := bizcal.New(time.UTC, bizcal.WithAlwaysOpen(), bizcal.WithRule(nil))
	if !errors.Is(err, bizcal.ErrInvalidRule) {
		t.Fatalf("New() = %v, want ErrInvalidRule", err)
	}
}

func TestNew_WithRule_InvalidRuleValidatedAtNew(t *testing.T) {
	_, err := bizcal.New(time.UTC,
		bizcal.WithAlwaysOpen(),
		bizcal.WithRule(bizcal.NthWeekday{Month: time.May, Weekday: time.Monday, N: 0}),
	)
	if !errors.Is(err, bizcal.ErrInvalidRule) {
		t.Fatalf("New() = %v, want ErrInvalidRule", err)
	}
}

func TestNew_WithRules_BulkForm(t *testing.T) {
	_, err := bizcal.New(time.UTC,
		bizcal.WithAlwaysOpen(),
		bizcal.WithRules(bizcal.Fixed{Month: time.January, Day: 1}, bizcal.Fixed{Month: time.July, Day: 4}),
	)
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
}

func TestNew_WithShifts_InvalidShift(t *testing.T) {
	start := time.Date(2026, time.July, 21, 17, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	_, err := bizcal.New(time.UTC, bizcal.WithShifts(bizcal.Shift(start, end)))
	if !errors.Is(err, bizcal.ErrInvalidShift) {
		t.Fatalf("New() = %v, want ErrInvalidShift", err)
	}
}

func TestNew_WithShifts_ZeroTimes(t *testing.T) {
	_, err := bizcal.New(time.UTC, bizcal.WithShifts(bizcal.Interval{}))
	if !errors.Is(err, bizcal.ErrInvalidShift) {
		t.Fatalf("New() = %v, want ErrInvalidShift", err)
	}
}

func TestNew_MultipleOptionErrors_AllPresent(t *testing.T) {
	_, err := bizcal.New(time.UTC,
		bizcal.WithWeekday(time.Weekday(9)),
		bizcal.WithRule(nil),
		bizcal.WithShifts(bizcal.Shift(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))),
	)
	if !errors.Is(err, bizcal.ErrInvalidWeekday) {
		t.Fatalf("New() = %v, want ErrInvalidWeekday present", err)
	}
	if !errors.Is(err, bizcal.ErrInvalidRule) {
		t.Fatalf("New() = %v, want ErrInvalidRule present", err)
	}
	if !errors.Is(err, bizcal.ErrInvalidShift) {
		t.Fatalf("New() = %v, want ErrInvalidShift present", err)
	}
}

func TestNew_WithExceptions_ShortDay_InvalidCapacity(t *testing.T) {
	cases := []struct {
		name     string
		capacity time.Duration
	}{
		{"negative", -time.Minute},
		{"over 24h", 25 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := bizcal.New(time.UTC,
				bizcal.WithAlwaysOpen(),
				bizcal.WithExceptions(bizcal.ShortDay(bizcal.MustDate(2026, time.December, 31), c.capacity)),
			)
			if !errors.Is(err, bizcal.ErrInvalidCapacity) {
				t.Fatalf("New() = %v, want ErrInvalidCapacity", err)
			}
		})
	}
}

func TestNew_WithExceptions_ShortDay_ZeroCapacityValid(t *testing.T) {
	_, err := bizcal.New(time.UTC,
		bizcal.WithAlwaysOpen(),
		bizcal.WithExceptions(bizcal.ShortDay(bizcal.MustDate(2026, time.December, 31), 0)),
	)
	if err != nil {
		t.Fatalf("New() = %v, want nil (zero capacity behaves as DayOff)", err)
	}
}

func TestNew_WithExceptions_CustomDayEmptyIsDayOff(t *testing.T) {
	// CustomDay with no windows is documented to behave as DayOff; this is
	// just a construction smoke test — behavioral verification is Task 4's.
	_, err := bizcal.New(time.UTC,
		bizcal.WithAlwaysOpen(),
		bizcal.WithExceptions(bizcal.CustomDay(bizcal.MustDate(2026, time.July, 21))),
	)
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
}

func TestNew_WithHorizon(t *testing.T) {
	_, err := bizcal.New(time.UTC, bizcal.WithAlwaysOpen(), bizcal.WithHorizon(48*time.Hour))
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
}

func TestNew_WithHorizon_Invalid(t *testing.T) {
	_, err := bizcal.New(time.UTC, bizcal.WithAlwaysOpen(), bizcal.WithHorizon(0))
	if !errors.Is(err, bizcal.ErrInvalidCapacity) {
		t.Fatalf("New() = %v, want ErrInvalidCapacity", err)
	}
	_, err = bizcal.New(time.UTC, bizcal.WithAlwaysOpen(), bizcal.WithHorizon(-time.Hour))
	if !errors.Is(err, bizcal.ErrInvalidCapacity) {
		t.Fatalf("New() = %v, want ErrInvalidCapacity", err)
	}
}

func TestCalendar_DateOf_ConvertsToCalendarZone(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	cal, err := bizcal.New(kyiv, bizcal.WithAlwaysOpen())
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}

	// 2026-07-21 00:30 Kyiv (UTC+3 in July) is 2026-07-20 21:30 UTC.
	instant := time.Date(2026, time.July, 20, 21, 30, 0, 0, time.UTC)
	got := cal.DateOf(instant)
	want := bizcal.MustDate(2026, time.July, 21)
	if got != want {
		t.Fatalf("DateOf(%v) = %v, want %v", instant, got, want)
	}
}

// TestNew_MutationSafety_ConstructionLevel is the construction-level half of
// the mutation-safety requirement: options must not alias the caller's
// slices. It only verifies New succeeds and is unaffected by post-New
// mutation of the caller's inputs at the API surface available today
// (Location/DateOf); once day/instant ops exist (Task 4) this should be
// extended to assert the resolved schedule is unchanged too. See also the
// white-box TestNew_ClonesCallerSlices in calendar_internal_test.go, which
// pins the same invariant directly against unexported state.
func TestNew_MutationSafety_ConstructionLevel(t *testing.T) {
	windows := bizcal.MustWindows("09:00-17:00")
	loc := time.UTC

	cal, err := bizcal.New(loc, bizcal.WithWeekday(time.Monday, windows...))
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}

	// Mutate the caller's backing slice after New returns.
	windows[0] = bizcal.MustWindows("00:00-01:00")[0]

	if cal.Location() != loc {
		t.Fatalf("Location() = %v, want %v (calendar corrupted by post-New mutation)", cal.Location(), loc)
	}
}
