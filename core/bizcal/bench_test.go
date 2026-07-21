package bizcal_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

// BenchmarkNew measures constructing a workdays calendar with three holiday
// rules registered (the New() cost, not any lazy year resolution).
func BenchmarkNew(b *testing.B) {
	for b.Loop() {
		_, err := bizcal.New(time.UTC,
			bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
			bizcal.WithRule(bizcal.Fixed{Month: time.January, Day: 1}),
			bizcal.WithRule(bizcal.Fixed{Month: time.July, Day: 4}),
			bizcal.WithRule(bizcal.NthWeekday{Month: time.November, Weekday: time.Thursday, N: 4}),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// windowsCalendar builds a Monday-Friday, 09:00-18:00 windows-model
// calendar used by the instant-op benchmarks below.
func windowsCalendar(b *testing.B) *bizcal.Calendar {
	b.Helper()
	windows := bizcal.MustWindows("09:00-18:00")
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWeekday(time.Monday, windows...),
		bizcal.WithWeekday(time.Tuesday, windows...),
		bizcal.WithWeekday(time.Wednesday, windows...),
		bizcal.WithWeekday(time.Thursday, windows...),
		bizcal.WithWeekday(time.Friday, windows...),
		bizcal.WithRule(bizcal.Fixed{Month: time.January, Day: 1}),
	)
	if err != nil {
		b.Fatal(err)
	}
	return cal
}

// BenchmarkIsOpen measures IsOpen against an already-resolved year (the
// resolver's steady-state cost, with no lazy year build in the loop).
func BenchmarkIsOpen(b *testing.B) {
	cal := windowsCalendar(b)
	t := time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC)
	cal.IsOpen(t) // prime the 2026 year plan
	b.ReportAllocs()

	for b.Loop() {
		cal.IsOpen(t)
	}
}

// BenchmarkNextOpen measures a weekend hop: querying Saturday noon on a
// Monday-Friday calendar, which must scan forward to Monday 09:00.
func BenchmarkNextOpen(b *testing.B) {
	cal := windowsCalendar(b)
	saturday := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	if _, err := cal.NextOpen(saturday); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()

	for b.Loop() {
		if _, err := cal.NextOpen(saturday); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAdd_Short measures Add with a budget that lands the same day.
func BenchmarkAdd_Short(b *testing.B) {
	cal := windowsCalendar(b)
	start := time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC)
	if _, err := cal.Add(start, 2*time.Hour); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()

	for b.Loop() {
		if _, err := cal.Add(start, 2*time.Hour); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAdd_MultiWeek measures Add with an 80h budget spanning multiple
// weeks and a holiday, exercising the multi-day scan loop.
func BenchmarkAdd_MultiWeek(b *testing.B) {
	cal := windowsCalendar(b)
	start := time.Date(2025, time.December, 29, 9, 0, 0, 0, time.UTC)
	if _, err := cal.Add(start, 80*time.Hour); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()

	for b.Loop() {
		if _, err := cal.Add(start, 80*time.Hour); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBetween_Day measures Between over a single-day span.
func BenchmarkBetween_Day(b *testing.B) {
	cal := windowsCalendar(b)
	a := time.Date(2026, time.July, 20, 9, 12, 0, 0, time.UTC)
	bnd := time.Date(2026, time.July, 20, 16, 5, 0, 0, time.UTC)
	cal.Between(a, bnd)
	b.ReportAllocs()

	for b.Loop() {
		cal.Between(a, bnd)
	}
}

// BenchmarkBetween_Month measures Between over a roughly month-long span.
func BenchmarkBetween_Month(b *testing.B) {
	cal := windowsCalendar(b)
	a := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	bnd := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	cal.Between(a, bnd)
	b.ReportAllocs()

	for b.Loop() {
		cal.Between(a, bnd)
	}
}

// BenchmarkScheduledBetween_Year measures ScheduledBetween over a full
// year on a workdays-model calendar.
func BenchmarkScheduledBetween_Year(b *testing.B) {
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
		bizcal.WithRule(bizcal.Fixed{Month: time.January, Day: 1}),
		bizcal.WithRule(bizcal.Fixed{Month: time.July, Day: 4}),
		bizcal.WithRule(bizcal.NthWeekday{Month: time.November, Weekday: time.Thursday, N: 4}),
	)
	if err != nil {
		b.Fatal(err)
	}
	from := bizcal.MustDate(2026, time.January, 1)
	to := bizcal.MustDate(2027, time.January, 1)
	cal.ScheduledBetween(from, to)
	b.ReportAllocs()

	for b.Loop() {
		cal.ScheduledBetween(from, to)
	}
}

// BenchmarkWindowsBetween_Month measures WindowsBetween over a month span,
// fully draining the returned iterator on every iteration.
func BenchmarkWindowsBetween_Month(b *testing.B) {
	cal := windowsCalendar(b)
	from := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	for range cal.WindowsBetween(from, to) {
	}
	b.ReportAllocs()

	for b.Loop() {
		for range cal.WindowsBetween(from, to) {
		}
	}
}
