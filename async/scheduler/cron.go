package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronAliases are the standard @-shortcuts, rewritten to 5-field specs.
var cronAliases = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

var monthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var dowNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// Cron parses a 5-field cron expression (minute hour day-of-month month
// day-of-week) evaluated in UTC. See CronIn for the field grammar.
func Cron(spec string) (Schedule, error) {
	return CronIn(spec, time.UTC)
}

// CronIn parses a 5-field cron expression evaluated in loc. Fields support
// `*`, values, ranges (`1-5`), steps (`*/15`, `1-30/5`, `5/10`), comma lists,
// month and weekday names (`jan`, `mon`), and the aliases @hourly, @daily,
// @midnight, @weekly, @monthly, @yearly, @annually. Day-of-month and
// day-of-week combine with the standard vixie OR rule: when both are
// restricted, a day matching either fires. Sunday is 0 or 7.
//
// Think twice before reaching for a zone with DST: ticks falling inside a
// spring-forward gap are skipped that day, and wall times repeated by a
// fall-back fire at both occurrences. UTC (Cron) avoids DST entirely.
func CronIn(spec string, loc *time.Location) (Schedule, error) {
	if loc == nil {
		return nil, fmt.Errorf("%w: %q: nil location", ErrInvalidSpec, spec)
	}
	expr := strings.TrimSpace(spec)
	if alias, ok := cronAliases[strings.ToLower(expr)]; ok {
		expr = alias
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("%w: %q: want 5 fields (minute hour day-of-month month day-of-week), got %d", ErrInvalidSpec, spec, len(fields))
	}
	s := &cronSchedule{loc: loc, domStar: fields[2] == "*", dowStar: fields[4] == "*"}
	var err error
	if s.minute, err = parseCronField(fields[0], 0, 59, nil); err != nil {
		return nil, fmt.Errorf("%w: %q: minute: %w", ErrInvalidSpec, spec, err)
	}
	if s.hour, err = parseCronField(fields[1], 0, 23, nil); err != nil {
		return nil, fmt.Errorf("%w: %q: hour: %w", ErrInvalidSpec, spec, err)
	}
	if s.dom, err = parseCronField(fields[2], 1, 31, nil); err != nil {
		return nil, fmt.Errorf("%w: %q: day-of-month: %w", ErrInvalidSpec, spec, err)
	}
	if s.month, err = parseCronField(fields[3], 1, 12, monthNames); err != nil {
		return nil, fmt.Errorf("%w: %q: month: %w", ErrInvalidSpec, spec, err)
	}
	// Day-of-week parses over 0-7 so ranges like "5-7" work; bit 7 (the vixie
	// second Sunday) folds into bit 0 below.
	if s.dow, err = parseCronField(fields[4], 0, 7, dowNames); err != nil {
		return nil, fmt.Errorf("%w: %q: day-of-week: %w", ErrInvalidSpec, spec, err)
	}
	if s.dow&(1<<7) != 0 {
		s.dow = s.dow&^(1<<7) | 1
	}
	return s, nil
}

// MustCron is Cron for package-level wiring: it panics on a malformed spec.
func MustCron(spec string) Schedule {
	s, err := Cron(spec)
	if err != nil {
		panic(err)
	}
	return s
}

// parseCronField parses one comma-separated field into a bitmask over lo..hi.
func parseCronField(expr string, lo, hi int, names map[string]int) (uint64, error) {
	var mask uint64
	for part := range strings.SplitSeq(expr, ",") {
		m, err := parseCronPart(part, lo, hi, names)
		if err != nil {
			return 0, err
		}
		mask |= m
	}
	return mask, nil
}

// parseCronPart parses one `*`, value, range, or step term.
func parseCronPart(part string, lo, hi int, names map[string]int) (uint64, error) {
	rangeExpr, stepExpr, hasStep := strings.Cut(part, "/")
	step := 1
	if hasStep {
		n, err := strconv.Atoi(stepExpr)
		if err != nil || n < 1 {
			return 0, fmt.Errorf("bad step %q", part)
		}
		step = n
	}
	from, to := lo, hi
	if rangeExpr != "*" {
		fromExpr, toExpr, isRange := strings.Cut(rangeExpr, "-")
		var err error
		if from, err = cronValue(fromExpr, names); err != nil {
			return 0, err
		}
		switch {
		case isRange:
			if to, err = cronValue(toExpr, names); err != nil {
				return 0, err
			}
		case hasStep:
			// vixie: "N/step" means N-max/step.
			to = hi
		default:
			to = from
		}
	}
	if from < lo || to > hi || from > to {
		return 0, fmt.Errorf("value out of range %d-%d in %q", lo, hi, part)
	}
	var mask uint64
	for v := from; v <= to; v += step {
		mask |= 1 << uint(v)
	}
	return mask, nil
}

// cronValue resolves a numeric or named field value.
func cronValue(s string, names map[string]int) (int, error) {
	if v, ok := names[strings.ToLower(s)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("bad value %q", s)
	}
	return v, nil
}

// cronSchedule matches times against per-field bitmasks in a fixed location.
type cronSchedule struct {
	loc     *time.Location
	minute  uint64
	hour    uint64
	dom     uint64
	month   uint64
	dow     uint64
	domStar bool
	dowStar bool
}

// Next implements Schedule: field-by-field advance (month, then day, hour,
// minute), resetting lower fields on every jump, bounded at five years out —
// far enough for any satisfiable spec (a Feb 29 schedule waits at most four).
func (s *cronSchedule) Next(t time.Time) time.Time {
	t = t.In(s.loc).Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		switch {
		case s.month&(1<<uint(t.Month())) == 0:
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, s.loc).AddDate(0, 1, 0)
		case !s.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, s.loc).AddDate(0, 0, 1)
		case s.hour&(1<<uint(t.Hour())) == 0:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, s.loc).Add(time.Hour)
		case s.minute&(1<<uint(t.Minute())) == 0:
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}

// dayMatches applies the vixie day rule: a `*` field mask is full, so AND is
// correct unless both fields are restricted — then a day matching either fires.
func (s *cronSchedule) dayMatches(t time.Time) bool {
	dom := s.dom&(1<<uint(t.Day())) != 0
	dow := s.dow&(1<<uint(t.Weekday())) != 0
	if !s.domStar && !s.dowStar {
		return dom || dow
	}
	return dom && dow
}
