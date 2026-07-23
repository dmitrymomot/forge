package bizcal

import (
	"errors"
	"fmt"
	"time"
)

// defaultHorizon bounds NextOpen/Add/AddWorkingDays scans when the caller
// does not set WithHorizon: 5 years of calendar time.
const defaultHorizon = 5 * 365 * 24 * time.Hour

// config accumulates the raw, caller-shaped inputs New receives from Option
// values before validation and normalization. It is never exposed outside
// the package.
type config struct {
	exceptions     map[Date]Exception
	weekly         [7][]Window
	rules          []Rule
	shifts         []Interval
	workdayCap     [7]time.Duration
	horizon        time.Duration
	workdaySet     [7]bool
	usedWeekday    bool
	usedWorkdays   bool
	usedAlwaysOpen bool
}

// Option configures a Calendar under construction. Option errors accumulate
// across every call and are returned joined from New.
type Option func(*config) error

// WithWeekday adds clock windows open on every occurrence of wd. It is
// repeatable per weekday: later calls for the same weekday append rather
// than replace. wd must be Sunday through Saturday, else ErrInvalidWeekday.
// WithWeekday establishes the calendar's base schedule and may not be
// combined with WithWorkdays or WithAlwaysOpen.
func WithWeekday(wd time.Weekday, windows ...Window) Option {
	return func(c *config) error {
		if wd < time.Sunday || wd > time.Saturday {
			return fmt.Errorf("%w: %d", ErrInvalidWeekday, wd)
		}
		c.usedWeekday = true
		ws := make([]Window, len(windows))
		copy(ws, windows)
		c.weekly[wd] = append(c.weekly[wd], ws...)
		return nil
	}
}

// WithWorkdays marks each listed weekday open for the whole civil day with
// scheduled capacity perDay (the HR day-granular model: no invented clock
// windows). perDay must be positive and at most 24h, else
// ErrInvalidCapacity; each weekday must be Sunday through Saturday, else
// ErrInvalidWeekday. WithWorkdays establishes the calendar's base schedule
// and may not be combined with WithWeekday or WithAlwaysOpen.
func WithWorkdays(perDay time.Duration, weekdays ...time.Weekday) Option {
	return func(c *config) error {
		if perDay <= 0 || perDay > 24*time.Hour {
			return fmt.Errorf("%w: %s", ErrInvalidCapacity, perDay)
		}
		for _, wd := range weekdays {
			if wd < time.Sunday || wd > time.Saturday {
				return fmt.Errorf("%w: %d", ErrInvalidWeekday, wd)
			}
		}
		c.usedWorkdays = true
		for _, wd := range weekdays {
			c.workdaySet[wd] = true
			c.workdayCap[wd] = perDay
		}
		return nil
	}
}

// WithAlwaysOpen marks every day open 00:00-24:00 with 24h capacity.
// WithAlwaysOpen establishes the calendar's base schedule and may not be
// combined with WithWeekday or WithWorkdays.
func WithAlwaysOpen() Option {
	return func(c *config) error {
		c.usedAlwaysOpen = true
		return nil
	}
}

// WithRule registers a holiday Rule (repeatable). r must be non-nil, else
// ErrInvalidRule; r's own field values are validated later, inside New.
func WithRule(r Rule) Option {
	return func(c *config) error {
		if r == nil {
			return fmt.Errorf("%w: nil rule", ErrInvalidRule)
		}
		c.rules = append(c.rules, r)
		return nil
	}
}

// WithRules is the bulk convenience form of WithRule.
func WithRules(rs ...Rule) Option {
	return func(c *config) error {
		var errs []error
		for _, r := range rs {
			if r == nil {
				errs = append(errs, fmt.Errorf("%w: nil rule", ErrInvalidRule))
				continue
			}
			c.rules = append(c.rules, r)
		}
		return errors.Join(errs...)
	}
}

// WithExceptions registers per-date plan overrides. If two exceptions share
// a date (whether from the same call or across repeated calls), the last
// one applied wins.
func WithExceptions(excs ...Exception) Option {
	return func(c *config) error {
		var errs []error
		for _, e := range excs {
			if e.kind == exceptionShortDay && (e.capacity < 0 || e.capacity > 24*time.Hour) {
				errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidCapacity, e.capacity))
				continue
			}
			if c.exceptions == nil {
				c.exceptions = make(map[Date]Exception, len(excs))
			}
			c.exceptions[e.date] = e
		}
		return errors.Join(errs...)
	}
}

// WithShifts registers absolute rostered coverage intervals (repeatable).
// Each Interval must have a non-zero Start and End, with End strictly after
// Start, else ErrInvalidShift. Shifts add open time on top of whatever the
// base/rules/exceptions resolve a day to; they are not a base source and
// may be combined with any base, or used with no base at all.
func WithShifts(ivs ...Interval) Option {
	return func(c *config) error {
		var errs []error
		for _, iv := range ivs {
			if iv.Start.IsZero() || iv.End.IsZero() || !iv.End.After(iv.Start) {
				errs = append(errs, fmt.Errorf("%w: start=%s end=%s", ErrInvalidShift, iv.Start, iv.End))
				continue
			}
			c.shifts = append(c.shifts, iv)
		}
		return errors.Join(errs...)
	}
}

// WithHorizon sets the scan cap NextOpen, Add, and AddWorkingDays search
// before giving up with ErrHorizonExceeded (default 5 years of calendar
// time). d must be positive.
func WithHorizon(d time.Duration) Option {
	return func(c *config) error {
		if d <= 0 {
			return fmt.Errorf("%w: horizon must be positive", ErrInvalidCapacity)
		}
		c.horizon = d
		return nil
	}
}

// exceptionKind distinguishes the three Exception shapes.
type exceptionKind int

const (
	exceptionDayOff exceptionKind = iota
	exceptionShortDay
	exceptionCustomDay
)

// Exception is an opaque per-date override of a Calendar's resolved day
// plan, constructed via DayOff, ShortDay, or CustomDay and registered with
// WithExceptions.
type Exception struct {
	windows  []Window
	date     Date
	kind     exceptionKind
	capacity time.Duration
}

// DayOff marks d as fully closed, overriding whatever the base/rules would
// otherwise resolve that date to.
func DayOff(d Date) Exception {
	return Exception{date: d, kind: exceptionDayOff}
}

// ShortDay marks d open for the whole civil day with scheduled capacity
// overridden to capacity (the workdays-model equivalent of a reduced-hours
// day). capacity must be non-negative and at most 24h, else New reports
// ErrInvalidCapacity; a capacity of 0 behaves exactly like DayOff.
func ShortDay(d Date, capacity time.Duration) Exception {
	return Exception{date: d, kind: exceptionShortDay, capacity: capacity}
}

// CustomDay replaces d's open windows with windows (the windows-model
// equivalent of ShortDay). Calling CustomDay with no windows is equivalent
// to DayOff.
func CustomDay(d Date, windows ...Window) Exception {
	if len(windows) == 0 {
		return Exception{date: d, kind: exceptionDayOff}
	}
	return Exception{date: d, kind: exceptionCustomDay, windows: mergeWindows(windows)}
}

// Shift constructs an Interval for use with WithShifts.
func Shift(start, end time.Time) Interval {
	return Interval{Start: start, End: end}
}
