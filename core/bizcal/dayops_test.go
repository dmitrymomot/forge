package bizcal_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

// --- fixture calendars ---------------------------------------------------

// calA is the HR workdays fixture: Mon-Fri 8h in Europe/Kyiv with New
// Year's Day as a holiday, 2026-07-24 taken as a day off, and 2026-12-31 a
// short (4h) day.
func calA(t *testing.T) *bizcal.Calendar {
	t.Helper()
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	cal, err := bizcal.New(kyiv,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
		bizcal.WithRule(bizcal.Fixed{Month: time.January, Day: 1}),
		bizcal.WithExceptions(
			bizcal.DayOff(bizcal.MustDate(2026, time.July, 24)),
			bizcal.ShortDay(bizcal.MustDate(2026, time.December, 31), 4*time.Hour),
		),
	)
	if err != nil {
		t.Fatalf("New(calA) = %v", err)
	}
	return cal
}

// calB is the weekly-windows fixture: Mon-Thu 09-13 + 14-18, Fri 09-15, in
// America/New_York.
func calB(t *testing.T) *bizcal.Calendar {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	split := bizcal.MustWindows("09:00-13:00", "14:00-18:00")
	cal, err := bizcal.New(ny,
		bizcal.WithWeekday(time.Monday, split...),
		bizcal.WithWeekday(time.Tuesday, split...),
		bizcal.WithWeekday(time.Wednesday, split...),
		bizcal.WithWeekday(time.Thursday, split...),
		bizcal.WithWeekday(time.Friday, bizcal.MustWindows("09:00-15:00")...),
	)
	if err != nil {
		t.Fatalf("New(calB) = %v", err)
	}
	return cal
}

// calC is the shifts-only roster fixture: no base schedule, a New Year's
// holiday rule, one cross-midnight shift (2026-07-20 22:00 -> 07-21 02:00
// UTC) and one shift landing on the holiday (2026-01-01 09:00-17:00 UTC).
func calC(t *testing.T) *bizcal.Calendar {
	t.Helper()
	cross := bizcal.Shift(
		time.Date(2026, time.July, 20, 22, 0, 0, 0, time.UTC),
		time.Date(2026, time.July, 21, 2, 0, 0, 0, time.UTC),
	)
	onHoliday := bizcal.Shift(
		time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC),
		time.Date(2026, time.January, 1, 17, 0, 0, 0, time.UTC),
	)
	cal, err := bizcal.New(time.UTC,
		bizcal.WithRule(bizcal.Fixed{Month: time.January, Day: 1}),
		bizcal.WithShifts(cross, onHoliday),
	)
	if err != nil {
		t.Fatalf("New(calC) = %v", err)
	}
	return cal
}

// calD is the always-open fixture.
func calD(t *testing.T) *bizcal.Calendar {
	t.Helper()
	cal, err := bizcal.New(time.UTC, bizcal.WithAlwaysOpen())
	if err != nil {
		t.Fatalf("New(calD) = %v", err)
	}
	return cal
}

// --- IsWorkingDay --------------------------------------------------------

func TestIsWorkingDay(t *testing.T) {
	a := calA(t)
	c := calC(t)
	d := calD(t)

	cases := []struct {
		name string
		cal  *bizcal.Calendar
		date bizcal.Date
		want bool
	}{
		{"workdays weekday", a, bizcal.MustDate(2026, time.July, 22), true},  // Wed
		{"workdays weekend", a, bizcal.MustDate(2026, time.July, 25), false}, // Sat
		{"workdays dayoff exception", a, bizcal.MustDate(2026, time.July, 24), false},
		{"workdays holiday", a, bizcal.MustDate(2026, time.January, 1), false},
		{"workdays shortday", a, bizcal.MustDate(2026, time.December, 31), true},
		{"shift on holiday", c, bizcal.MustDate(2026, time.January, 1), true},
		{"shift cross-midnight tail", c, bizcal.MustDate(2026, time.July, 21), true},
		{"roster empty day", c, bizcal.MustDate(2026, time.July, 15), false},
		{"always open", d, bizcal.MustDate(2026, time.July, 25), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cal.IsWorkingDay(tc.date); got != tc.want {
				t.Fatalf("IsWorkingDay(%s) = %v, want %v", tc.date, got, tc.want)
			}
		})
	}
}

// --- WorkingDays ---------------------------------------------------------

func TestWorkingDays_JulyWorkdays(t *testing.T) {
	a := calA(t)
	// July 2026 has 23 weekdays; 07-24 is a day-off exception, leaving 22.
	got := a.WorkingDays(bizcal.MustDate(2026, time.July, 1), bizcal.MustDate(2026, time.August, 1))
	if got != 22 {
		t.Fatalf("WorkingDays(Jul) = %d, want 22", got)
	}
}

func TestWorkingDays_EmptyAndInverted(t *testing.T) {
	a := calA(t)
	jul1 := bizcal.MustDate(2026, time.July, 1)
	aug1 := bizcal.MustDate(2026, time.August, 1)
	if got := a.WorkingDays(jul1, jul1); got != 0 {
		t.Fatalf("WorkingDays(empty) = %d, want 0", got)
	}
	if got := a.WorkingDays(aug1, jul1); got != 0 {
		t.Fatalf("WorkingDays(inverted) = %d, want 0", got)
	}
}

// --- AddWorkingDays ------------------------------------------------------

func TestAddWorkingDays(t *testing.T) {
	a := calA(t)
	cases := []struct {
		name string
		from bizcal.Date
		n    int
		want bizcal.Date
	}{
		// 07-23 Thu +1: 07-24 is off, 07-25/26 weekend, so 07-27 Mon.
		{"skip weekend and holiday", bizcal.MustDate(2026, time.July, 23), 1, bizcal.MustDate(2026, time.July, 27)},
		// 07-27 Mon -1: back over weekend + 07-24 off to 07-23 Thu.
		{"negative walks back", bizcal.MustDate(2026, time.July, 27), -1, bizcal.MustDate(2026, time.July, 23)},
		// n==0 is identity even on a non-working day.
		{"zero identity on dayoff", bizcal.MustDate(2026, time.July, 24), 0, bizcal.MustDate(2026, time.July, 24)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := a.AddWorkingDays(tc.from, tc.n)
			if err != nil {
				t.Fatalf("AddWorkingDays(%s, %d) = %v", tc.from, tc.n, err)
			}
			if got != tc.want {
				t.Fatalf("AddWorkingDays(%s, %d) = %s, want %s", tc.from, tc.n, got, tc.want)
			}
		})
	}
}

func TestAddWorkingDays_HorizonExceeded(t *testing.T) {
	// A calendar whose rule closes every day of every year: no working day
	// is ever reachable, so AddWorkingDays must exhaust the horizon.
	allDays := bizcal.RuleFunc(func(year int) []bizcal.Date {
		var ds []bizcal.Date
		d := bizcal.MustDate(year, time.January, 1)
		end := bizcal.MustDate(year+1, time.January, 1)
		for d.Before(end) {
			ds = append(ds, d)
			d = d.AddDays(1)
		}
		return ds
	})
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
		bizcal.WithRule(allDays),
		bizcal.WithHorizon(10*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if _, err := cal.AddWorkingDays(bizcal.MustDate(2026, time.July, 1), 1); !errors.Is(err, bizcal.ErrHorizonExceeded) {
		t.Fatalf("AddWorkingDays() = %v, want ErrHorizonExceeded", err)
	}
	if _, err := cal.AddWorkingDays(bizcal.MustDate(2026, time.July, 1), -1); !errors.Is(err, bizcal.ErrHorizonExceeded) {
		t.Fatalf("AddWorkingDays(backward) = %v, want ErrHorizonExceeded", err)
	}
}

func TestRule_OutOfYearDatesIgnored(t *testing.T) {
	// A rule that always returns the same far-year date regardless of the
	// queried year must only close that date in its own year. buildYear now
	// evaluates each rule for years y-1, y, and y+1, so 2030-06-18 (a Tuesday)
	// is produced when building 2029, 2030, and 2031; only 2030's plan may
	// keep it. This pins the defensive out-of-year filter: 2029-06-18 (Monday)
	// and 2031-06-18 (Wednesday), both workdays, stay working days.
	fixedFar := bizcal.RuleFunc(func(int) []bizcal.Date {
		return []bizcal.Date{bizcal.MustDate(2030, time.June, 18)}
	})
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
		bizcal.WithRule(fixedFar),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if cal.IsWorkingDay(bizcal.MustDate(2030, time.June, 18)) {
		t.Fatal("IsWorkingDay(2030-06-18) = true, want false (rule closes its own year)")
	}
	if !cal.IsWorkingDay(bizcal.MustDate(2029, time.June, 18)) {
		t.Fatal("IsWorkingDay(2029-06-18) = false, want true (out-of-year rule date must be ignored)")
	}
	if !cal.IsWorkingDay(bizcal.MustDate(2031, time.June, 18)) {
		t.Fatal("IsWorkingDay(2031-06-18) = false, want true (out-of-year rule date must be ignored)")
	}
}

func TestRule_ObservedShiftBackwardAcrossYearBoundary(t *testing.T) {
	// Observed(Fixed{Jan 1}): in 2022 Jan 1 is a Saturday, so the observance
	// shifts back to Friday 2021-12-31, which lands in the prior year's plan.
	// buildYear(2021) must honor it even though the rule was defined for 2022.
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
		bizcal.WithRule(bizcal.Observed(bizcal.Fixed{Month: time.January, Day: 1})),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if cal.IsWorkingDay(bizcal.MustDate(2021, time.December, 31)) {
		t.Fatal("IsWorkingDay(2021-12-31) = true, want false (observed New Year shifted back)")
	}
}

func TestRule_ObservedShiftForwardAcrossYearBoundary(t *testing.T) {
	// Observed(Fixed{Dec 31}): in 2023 Dec 31 is a Sunday, so the observance
	// shifts forward to Monday 2024-01-01, landing in the next year's plan.
	// buildYear(2024) must honor it even though the rule was defined for 2023.
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
		bizcal.WithRule(bizcal.Observed(bizcal.Fixed{Month: time.December, Day: 31})),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if cal.IsWorkingDay(bizcal.MustDate(2024, time.January, 1)) {
		t.Fatal("IsWorkingDay(2024-01-01) = true, want false (observed Dec 31 shifted forward)")
	}
}

// --- DayDuration ---------------------------------------------------------

func TestDayDuration_Workdays(t *testing.T) {
	a := calA(t)
	cases := []struct {
		name string
		date bizcal.Date
		want time.Duration
	}{
		{"weekday", bizcal.MustDate(2026, time.July, 22), 8 * time.Hour},
		{"weekend", bizcal.MustDate(2026, time.July, 25), 0},
		{"shortday", bizcal.MustDate(2026, time.December, 31), 4 * time.Hour},
		{"holiday", bizcal.MustDate(2026, time.January, 1), 0},
		{"dayoff", bizcal.MustDate(2026, time.July, 24), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.DayDuration(tc.date); got != tc.want {
				t.Fatalf("DayDuration(%s) = %s, want %s", tc.date, got, tc.want)
			}
		})
	}
}

func TestDayDuration_Windows(t *testing.T) {
	b := calB(t)
	cases := []struct {
		name string
		date bizcal.Date
		want time.Duration
	}{
		{"monday split", bizcal.MustDate(2026, time.July, 6), 8 * time.Hour}, // 09-13 + 14-18
		{"friday", bizcal.MustDate(2026, time.July, 3), 6 * time.Hour},       // 09-15
		{"saturday", bizcal.MustDate(2026, time.July, 4), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.DayDuration(tc.date); got != tc.want {
				t.Fatalf("DayDuration(%s) = %s, want %s", tc.date, got, tc.want)
			}
		})
	}
}

func TestDayDuration_DST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	// A window straddling the 02:00 transition: 00:00-06:00 on Sundays.
	straddle, err := bizcal.New(ny, bizcal.WithWeekday(time.Sunday, bizcal.MustWindows("00:00-06:00")...))
	if err != nil {
		t.Fatalf("New(straddle) = %v", err)
	}
	// A window entirely after the 02:00 transition: 09:00-17:00 on Sundays.
	outside, err := bizcal.New(ny, bizcal.WithWeekday(time.Sunday, bizcal.MustWindows("09:00-17:00")...))
	if err != nil {
		t.Fatalf("New(outside) = %v", err)
	}

	cases := []struct {
		name string
		cal  *bizcal.Calendar
		date bizcal.Date
		want time.Duration
	}{
		{"spring forward shrinks 1h", straddle, bizcal.MustDate(2026, time.March, 8), 5 * time.Hour},
		{"fall back grows 1h", straddle, bizcal.MustDate(2026, time.November, 1), 7 * time.Hour},
		{"normal sunday", straddle, bizcal.MustDate(2026, time.March, 15), 6 * time.Hour},
		{"window outside transition unaffected", outside, bizcal.MustDate(2026, time.March, 8), 8 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cal.DayDuration(tc.date); got != tc.want {
				t.Fatalf("DayDuration(%s) = %s, want %s", tc.date, got, tc.want)
			}
		})
	}
}

func TestDayDuration_CrossMidnightShiftSplits(t *testing.T) {
	c := calC(t)
	// The 22:00->02:00 shift contributes 2h to each of the two civil dates.
	if got := c.DayDuration(bizcal.MustDate(2026, time.July, 20)); got != 2*time.Hour {
		t.Fatalf("DayDuration(07-20) = %s, want 2h", got)
	}
	if got := c.DayDuration(bizcal.MustDate(2026, time.July, 21)); got != 2*time.Hour {
		t.Fatalf("DayDuration(07-21) = %s, want 2h", got)
	}
	// The shift landing on the holiday still counts as capacity.
	if got := c.DayDuration(bizcal.MustDate(2026, time.January, 1)); got != 8*time.Hour {
		t.Fatalf("DayDuration(01-01) = %s, want 8h", got)
	}
}

// --- ScheduledBetween ----------------------------------------------------

func TestScheduledBetween_July(t *testing.T) {
	a := calA(t)
	// 22 working days at 8h each.
	got := a.ScheduledBetween(bizcal.MustDate(2026, time.July, 1), bizcal.MustDate(2026, time.August, 1))
	if got != 176*time.Hour {
		t.Fatalf("ScheduledBetween(Jul) = %s, want 176h", got)
	}
}

func TestScheduledBetween_EmptyAndInverted(t *testing.T) {
	a := calA(t)
	jul1 := bizcal.MustDate(2026, time.July, 1)
	aug1 := bizcal.MustDate(2026, time.August, 1)
	if got := a.ScheduledBetween(jul1, jul1); got != 0 {
		t.Fatalf("ScheduledBetween(empty) = %s, want 0", got)
	}
	if got := a.ScheduledBetween(aug1, jul1); got != 0 {
		t.Fatalf("ScheduledBetween(inverted) = %s, want 0", got)
	}
}

// --- mutation safety (behavioral half, deferred from Task 3) -------------

func TestMutationSafety_Behavioral(t *testing.T) {
	windows := bizcal.MustWindows("09:00-17:00")
	// A Tue->Wed cross-midnight shift; neither date carries a Monday base
	// window, so the shift's capacity is observable in isolation.
	start := time.Date(2026, time.July, 21, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.July, 22, 2, 0, 0, 0, time.UTC)
	shifts := []bizcal.Interval{bizcal.Shift(start, end)}

	cal, err := bizcal.New(time.UTC,
		bizcal.WithWeekday(time.Monday, windows...),
		bizcal.WithShifts(shifts...),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	// Mutate the caller's backing arrays after New returns.
	windows[0] = bizcal.MustWindows("00:00-01:00")[0]
	shifts[0] = bizcal.Shift(
		time.Date(1999, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1999, time.January, 1, 1, 0, 0, 0, time.UTC),
	)

	// Monday window is unchanged (8h), and the shift still splits 2h/2h.
	if got := cal.DayDuration(bizcal.MustDate(2026, time.July, 6)); got != 8*time.Hour {
		t.Fatalf("DayDuration(Mon) = %s, want 8h (mutation leaked)", got)
	}
	if got := cal.DayDuration(bizcal.MustDate(2026, time.July, 21)); got != 2*time.Hour {
		t.Fatalf("DayDuration(07-21) = %s, want 2h (shift mutation leaked)", got)
	}
	if got := cal.DayDuration(bizcal.MustDate(2026, time.July, 22)); got != 2*time.Hour {
		t.Fatalf("DayDuration(07-22) = %s, want 2h (shift mutation leaked)", got)
	}
}
