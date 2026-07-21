package bizcal

import (
	"fmt"
	"time"
)

// Rule produces the set of civil dates a holiday falls on in a given year.
// A year may yield zero dates (e.g. a Feb 29 rule in a non-leap year) or,
// in principle, more than one.
type Rule interface {
	Dates(year int) []Date
}

// Fixed is a holiday that falls on the same month and day every year, such
// as New Year's Day (January 1). Fixed{February, 29} is valid and yields a
// date only in leap years.
type Fixed struct {
	Month time.Month
	Day   int
}

// Dates returns the single date f falls on in year, or nil if the
// month/day combination does not exist in that year (e.g. Feb 29 in a
// non-leap year).
func (f Fixed) Dates(year int) []Date {
	d, err := NewDate(year, f.Month, f.Day)
	if err != nil {
		return nil
	}
	return []Date{d}
}

// validate reports whether f's month/day combination can ever occur on the
// calendar. It checks against year 2000, a leap year, so Fixed{February, 29}
// validates successfully; Fixed{February, 30} does not.
func (f Fixed) validate() error {
	if _, err := NewDate(2000, f.Month, f.Day); err != nil {
		return fmt.Errorf("%w: Fixed{Month: %s, Day: %d}", ErrInvalidRule, f.Month, f.Day)
	}
	return nil
}

// NthWeekday is a holiday defined by its ordinal weekday within a month,
// such as Thanksgiving (fourth Thursday in November). N counts from the
// start of the month when positive (N=1 is the first occurrence, N=4 the
// fourth) and from the end when negative (N=-1 is the last occurrence).
// N==0 is invalid.
type NthWeekday struct {
	Month   time.Month
	Weekday time.Weekday
	N       int
}

// Dates returns the single date nw falls on in year, or nil if the
// requested ordinal occurrence does not exist that month (e.g. a fifth
// occurrence of a weekday that only happens four times).
func (nw NthWeekday) Dates(year int) []Date {
	switch {
	case nw.N > 0:
		return nw.datesFromStart(year)
	case nw.N < 0:
		return nw.datesFromEnd(year)
	default:
		return nil
	}
}

func (nw NthWeekday) datesFromStart(year int) []Date {
	first := Date{Year: year, Month: nw.Month, Day: 1}
	offset := (int(nw.Weekday) - int(first.Weekday()) + 7) % 7
	day := 1 + offset + (nw.N-1)*7
	d, err := NewDate(year, nw.Month, day)
	if err != nil {
		return nil
	}
	return []Date{d}
}

func (nw NthWeekday) datesFromEnd(year int) []Date {
	lastDay := time.Date(year, nw.Month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	last := Date{Year: year, Month: nw.Month, Day: lastDay}
	offset := (int(last.Weekday()) - int(nw.Weekday) + 7) % 7
	day := lastDay - offset - (-nw.N-1)*7
	d, err := NewDate(year, nw.Month, day)
	if err != nil {
		return nil
	}
	return []Date{d}
}

// validate reports whether nw's fields are within range: N must be nonzero
// and at most 5 occurrences from either end (no month has more than five
// occurrences of a given weekday), and Month/Weekday must be in their
// normal ranges.
func (nw NthWeekday) validate() error {
	if nw.N == 0 || nw.N > 5 || nw.N < -5 {
		return fmt.Errorf("%w: NthWeekday{N: %d}: N must be nonzero and |N|<=5", ErrInvalidRule, nw.N)
	}
	if nw.Month < time.January || nw.Month > time.December {
		return fmt.Errorf("%w: NthWeekday{Month: %d}: out of range", ErrInvalidRule, nw.Month)
	}
	if nw.Weekday < time.Sunday || nw.Weekday > time.Saturday {
		return fmt.Errorf("%w: NthWeekday{Weekday: %d}: out of range", ErrInvalidRule, nw.Weekday)
	}
	return nil
}

// RuleFunc adapts a plain function to the Rule interface, for holidays
// whose dates cannot be expressed as Fixed or NthWeekday (e.g. computed
// religious observances). Dates it returns for a year other than the one
// requested are ignored by the resolver.
type RuleFunc func(year int) []Date

// Dates calls f(year) and returns the result.
func (f RuleFunc) Dates(year int) []Date {
	return f(year)
}

// Observed wraps r so that any date it produces falling on a Saturday is
// shifted back to the preceding Friday, and any date falling on a Sunday is
// shifted forward to the following Monday. Dates on other weekdays pass
// through unchanged. This is the common observed-holiday convention for
// weekend dates.
func Observed(r Rule) Rule {
	return observedRule{inner: r}
}

type observedRule struct {
	inner Rule
}

func (o observedRule) Dates(year int) []Date {
	dates := o.inner.Dates(year)
	if dates == nil {
		return nil
	}
	out := make([]Date, len(dates))
	for i, d := range dates {
		out[i] = observe(d)
	}
	return out
}

// observe shifts d off a weekend per the Observed convention.
func observe(d Date) Date {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDays(-1)
	case time.Sunday:
		return d.AddDays(1)
	default:
		return d
	}
}

// validateRule reports whether r is a validly configured Rule. Fixed and
// NthWeekday values (the two struct rule kinds tenant config can hold) are
// checked via their validate methods; RuleFunc and Observed-wrapped rules
// always validate ok, since a RuleFunc's correctness is a property of the
// Go code that defines it, not of serializable data. Unknown Rule
// implementations (custom types provided by a caller) also validate ok for
// the same reason.
func validateRule(r Rule) error {
	switch v := r.(type) {
	case Fixed:
		return v.validate()
	case NthWeekday:
		return v.validate()
	case observedRule:
		return validateRule(v.inner)
	default:
		return nil
	}
}
