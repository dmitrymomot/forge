package bizcal_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

func TestFixed_Dates_OrdinaryDate(t *testing.T) {
	r := bizcal.Fixed{Month: time.January, Day: 1}
	got := r.Dates(2026)
	want := []Date{bizcal.MustDate(2026, time.January, 1)}
	assertDates(t, got, want)
}

func TestFixed_Dates_Feb29OnlyInLeapYears(t *testing.T) {
	r := bizcal.Fixed{Month: time.February, Day: 29}

	if got := r.Dates(2026); len(got) != 0 {
		t.Fatalf("Fixed{Feb,29}.Dates(2026) = %v, want empty", got)
	}

	got := r.Dates(2028)
	want := []Date{bizcal.MustDate(2028, time.February, 29)}
	assertDates(t, got, want)
}

func TestNthWeekday_Dates_FromStart(t *testing.T) {
	r := bizcal.NthWeekday{Month: time.November, Weekday: time.Thursday, N: 4}
	got := r.Dates(2026)
	want := []Date{bizcal.MustDate(2026, time.November, 26)}
	assertDates(t, got, want)
}

func TestNthWeekday_Dates_FromEnd(t *testing.T) {
	r := bizcal.NthWeekday{Month: time.May, Weekday: time.Monday, N: -1}
	got := r.Dates(2026)
	want := []Date{bizcal.MustDate(2026, time.May, 25)}
	assertDates(t, got, want)
}

func TestNthWeekday_Dates_FifthOccurrence(t *testing.T) {
	r := bizcal.NthWeekday{Month: time.February, Weekday: time.Sunday, N: 5}

	if got := r.Dates(2026); len(got) != 0 {
		t.Fatalf("NthWeekday{Feb,Sunday,5}.Dates(2026) = %v, want empty (no 5th Sunday)", got)
	}

	// February 2032 has 5 Sundays: Feb 1, 8, 15, 22, 29 (2032 is a leap year, Feb 1 is a Sunday).
	got := r.Dates(2032)
	want := []Date{bizcal.MustDate(2032, time.February, 29)}
	assertDates(t, got, want)
}

func TestObserved_SaturdayShiftsToFriday(t *testing.T) {
	r := bizcal.Observed(bizcal.Fixed{Month: time.July, Day: 4})
	got := r.Dates(2026)
	want := []Date{bizcal.MustDate(2026, time.July, 3)}
	assertDates(t, got, want)
}

func TestObserved_SundayShiftsToMonday(t *testing.T) {
	// 2027-01-03 is a Sunday.
	r := bizcal.Observed(bizcal.Fixed{Month: time.January, Day: 3})
	got := r.Dates(2027)
	want := []Date{bizcal.MustDate(2027, time.January, 4)}
	assertDates(t, got, want)
}

func TestObserved_WeekdayUnchanged(t *testing.T) {
	// 2026-07-21 is a Tuesday.
	r := bizcal.Observed(bizcal.Fixed{Month: time.July, Day: 21})
	got := r.Dates(2026)
	want := []Date{bizcal.MustDate(2026, time.July, 21)}
	assertDates(t, got, want)
}

func TestRuleFunc_Passthrough(t *testing.T) {
	want := []Date{bizcal.MustDate(2026, time.March, 3), bizcal.MustDate(2026, time.March, 4)}
	r := bizcal.RuleFunc(func(year int) []bizcal.Date {
		return want
	})
	got := r.Dates(2026)
	assertDates(t, got, want)
}

func assertDates(t *testing.T, got, want []Date) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Dates() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Dates()[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}
