package bizcal

import (
	"errors"
	"maps"
	"sort"
	"sync"
	"time"
)

// Calendar is an immutable, goroutine-safe business-calendar built from a
// base schedule (weekly windows, day-granular workdays, or always-open),
// holiday rules, per-date exceptions, and rostered shifts. Construct one
// with New; every field is unexported and normalized at construction time,
// so a Calendar never aliases any slice or map the caller passed to its
// Options.
type Calendar struct {
	loc            *time.Location
	exceptions     map[Date]Exception
	years          map[int]*yearPlan
	weekly         [7][]Window
	rules          []Rule
	shifts         []Interval
	workdayCap     [7]time.Duration
	horizon        time.Duration
	mu             sync.RWMutex
	workdaySet     [7]bool
	usedWeekday    bool
	usedWorkdays   bool
	usedAlwaysOpen bool
}

// New builds a Calendar from loc and opts. loc must be non-nil, else
// ErrNilLocation. Every Option error and every registered Rule's validation
// error is collected and returned together via errors.Join.
//
// Exactly one base source may be used: WithWeekday, WithWorkdays, or
// WithAlwaysOpen; combining more than one is a construction error. A
// Calendar with no base and no shifts is statically closed on every day and
// is rejected with ErrNeverOpen.
func New(loc *time.Location, opts ...Option) (*Calendar, error) {
	var errs []error
	if loc == nil {
		errs = append(errs, ErrNilLocation)
	}

	cfg := &config{horizon: defaultHorizon}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			errs = append(errs, err)
		}
	}

	baseCount := 0
	if cfg.usedWeekday {
		baseCount++
	}
	if cfg.usedWorkdays {
		baseCount++
	}
	if cfg.usedAlwaysOpen {
		baseCount++
	}
	if baseCount > 1 {
		errs = append(errs, errors.New("bizcal: at most one base source (WithWeekday, WithWorkdays, WithAlwaysOpen) may be used"))
	}

	for _, r := range cfg.rules {
		if err := validateRule(r); err != nil {
			errs = append(errs, err)
		}
	}

	// A WithWeekday base only opens the calendar if at least one window was
	// actually supplied; WithWeekday(wd) with no windows contributes no open
	// time, so it does not count as a base for the never-open check.
	weekdayHasWindows := false
	for _, ws := range cfg.weekly {
		if len(ws) > 0 {
			weekdayHasWindows = true
			break
		}
	}
	hasBase := cfg.usedWorkdays || cfg.usedAlwaysOpen || (cfg.usedWeekday && weekdayHasWindows)
	if !hasBase && len(cfg.shifts) == 0 {
		errs = append(errs, ErrNeverOpen)
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	cal := &Calendar{
		loc:            loc,
		workdaySet:     cfg.workdaySet,
		workdayCap:     cfg.workdayCap,
		usedWeekday:    cfg.usedWeekday,
		usedWorkdays:   cfg.usedWorkdays,
		usedAlwaysOpen: cfg.usedAlwaysOpen,
		rules:          append([]Rule(nil), cfg.rules...),
		exceptions:     make(map[Date]Exception, len(cfg.exceptions)),
		shifts:         mergeIntervals(cfg.shifts),
		horizon:        cfg.horizon,
		years:          make(map[int]*yearPlan),
	}
	for i := range cfg.weekly {
		cal.weekly[i] = mergeWindows(cfg.weekly[i])
	}
	maps.Copy(cal.exceptions, cfg.exceptions)

	return cal, nil
}

// Location returns the *time.Location c interprets civil dates and clock
// windows in.
func (c *Calendar) Location() *time.Location {
	return c.loc
}

// DateOf converts t to c's zone and returns its civil date there.
func (c *Calendar) DateOf(t time.Time) Date {
	return DateOf(t.In(c.loc))
}

// IsWorkingDay reports whether d has any open time: a non-zero scheduled
// capacity or at least one open interval. A workdays-model day-off (or a
// zero-capacity ShortDay) leaves both empty and is therefore not a working
// day; a shift rostered onto a holiday makes that date working.
func (c *Calendar) IsWorkingDay(d Date) bool {
	p := c.dayPlan(d)
	return p.capacity > 0 || len(p.intervals) > 0
}

// WorkingDays counts the working days in the half-open range [from, to). It
// returns 0 when from is not before to (empty or inverted range).
func (c *Calendar) WorkingDays(from, to Date) int {
	if !from.Before(to) {
		return 0
	}
	count := 0
	for d := from; d.Before(to); d = d.AddDays(1) {
		if c.IsWorkingDay(d) {
			count++
		}
	}
	return count
}

// AddWorkingDays returns the n-th working day strictly after d (n > 0) or
// strictly before d (n < 0), skipping non-working days. n == 0 returns d
// unchanged, even when d itself is not a working day. The scan is bounded by
// the configured horizon (interpreted as a day count of horizon/24h from d);
// exhausting it returns ErrHorizonExceeded.
func (c *Calendar) AddWorkingDays(d Date, n int) (Date, error) {
	if n == 0 {
		return d, nil
	}
	maxDays := int(c.horizon / (24 * time.Hour))
	step := 1
	if n < 0 {
		step = -1
		n = -n
	}
	cur := d
	for moved := 0; ; {
		cur = cur.AddDays(step)
		moved++
		if moved > maxDays {
			return Date{}, ErrHorizonExceeded
		}
		if c.IsWorkingDay(cur) {
			if n--; n == 0 {
				return cur, nil
			}
		}
	}
}

// DayDuration returns d's scheduled capacity: the duration that counts as the
// day's expected hours. For the windows model this is the sum of real
// interval durations (so a DST day may differ from a nominal one); for the
// workdays model it is the fixed per-day capacity; shift time rostered onto d
// adds on top.
func (c *Calendar) DayDuration(d Date) time.Duration {
	return c.dayPlan(d).capacity
}

// ScheduledBetween sums DayDuration over the half-open range [from, to). It
// returns 0 when from is not before to (empty or inverted range).
func (c *Calendar) ScheduledBetween(from, to Date) time.Duration {
	if !from.Before(to) {
		return 0
	}
	var total time.Duration
	for d := from; d.Before(to); d = d.AddDays(1) {
		total += c.dayPlan(d).capacity
	}
	return total
}

// mergeWindows returns a sorted, non-aliasing copy of ws with overlapping or
// adjacent windows merged. A nil or empty ws yields nil.
func mergeWindows(ws []Window) []Window {
	if len(ws) == 0 {
		return nil
	}
	out := make([]Window, len(ws))
	copy(out, ws)
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })

	merged := out[:1]
	for _, w := range out[1:] {
		last := &merged[len(merged)-1]
		if w.start <= last.end {
			if w.end > last.end {
				last.end = w.end
			}
			continue
		}
		merged = append(merged, w)
	}
	return merged
}

// mergeIntervals returns a sorted, non-aliasing copy of ivs with overlapping
// or adjacent intervals merged (by absolute time). A nil or empty ivs
// yields nil.
func mergeIntervals(ivs []Interval) []Interval {
	if len(ivs) == 0 {
		return nil
	}
	out := make([]Interval, len(ivs))
	copy(out, ivs)
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })

	merged := out[:1]
	for _, iv := range out[1:] {
		last := &merged[len(merged)-1]
		if !iv.Start.After(last.End) {
			if iv.End.After(last.End) {
				last.End = iv.End
			}
			continue
		}
		merged = append(merged, iv)
	}
	return merged
}
