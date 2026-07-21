package bizcal

import (
	"errors"
	"maps"
	"sort"
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

	hasBase := cfg.usedWeekday || cfg.usedWorkdays || cfg.usedAlwaysOpen
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
