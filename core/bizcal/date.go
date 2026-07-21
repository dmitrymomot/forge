package bizcal

import (
	"fmt"
	"time"
)

// Date is a civil (calendar) date: a year, month, and day with no time-of-day
// or location component. The zero value is not a valid date; use NewDate,
// MustDate, or DateOf to construct one.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate validates and constructs a Date. It rejects impossible dates
// (e.g. 2026-02-30, month 13) by round-tripping through time.Date in UTC
// and checking the components survive unchanged.
func NewDate(year int, month time.Month, day int) (Date, error) {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day {
		return Date{}, fmt.Errorf("%w: %04d-%02d-%02d", ErrInvalidDate, year, month, day)
	}
	return Date{Year: year, Month: month, Day: day}, nil
}

// MustDate is like NewDate but panics if the date is invalid. Intended for
// literals known to be valid at compile time (tests, constants).
func MustDate(year int, month time.Month, day int) Date {
	d, err := NewDate(year, month, day)
	if err != nil {
		panic(err)
	}
	return d
}

// DateOf returns the civil date of t, read in t's own location. To convert
// an instant into a particular zone first, call t.In(loc) before DateOf.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// AddDays returns the date n civil days after d (n may be negative or zero).
// Arithmetic is performed via time.Date in UTC, which normalizes overflowing
// month/day components (including leap years) automatically.
func (d Date) AddDays(n int) Date {
	t := time.Date(d.Year, d.Month, d.Day+n, 0, 0, 0, 0, time.UTC)
	return DateOf(t)
}

// Weekday returns the day of the week for d. This is a pure civil
// computation independent of any time zone.
func (d Date) Weekday() time.Weekday {
	t := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
	return t.Weekday()
}

// Before reports whether d is strictly before o.
func (d Date) Before(o Date) bool {
	return d.Compare(o) < 0
}

// After reports whether d is strictly after o.
func (d Date) After(o Date) bool {
	return d.Compare(o) > 0
}

// Compare returns -1, 0, or +1 depending on whether d is before, equal to,
// or after o.
func (d Date) Compare(o Date) int {
	switch {
	case d.Year != o.Year:
		return cmp(d.Year, o.Year)
	case d.Month != o.Month:
		return cmp(int(d.Month), int(o.Month))
	default:
		return cmp(d.Day, o.Day)
	}
}

func cmp(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// IsZero reports whether d is the zero Date value.
func (d Date) IsZero() bool {
	return d == Date{}
}

// String formats d as "2026-07-21" (RFC 3339 date, zero-padded).
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}
