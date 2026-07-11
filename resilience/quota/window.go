package quota

import (
	"fmt"
	"strconv"
	"time"
)

// Unit is a calendar window granularity.
type Unit int

const (
	Daily Unit = iota
	Weekly
	Monthly
)

// Unlimited marks a Limit with no hard ceiling (pay-as-you-go / pure metering).
const Unlimited int64 = -1

// Window maps now to the current period's key suffix and its reset time. period
// is "" for gauges (no suffix); reset is the zero Time for gauges.
type Window func(subject string, now time.Time) (period string, reset time.Time)

// Calendar returns a Window aligned to calendar boundaries in loc (nil => UTC).
func Calendar(unit Unit, loc *time.Location) Window {
	if loc == nil {
		loc = time.UTC
	}
	return func(_ string, now time.Time) (string, time.Time) {
		n := now.In(loc)
		switch unit {
		case Daily:
			start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
			return start.Format("2006-01-02"), start.AddDate(0, 0, 1)
		case Weekly:
			offset := (int(n.Weekday()) + 6) % 7 // ISO: Monday = 0
			start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -offset)
			y, w := start.ISOWeek()
			return fmt.Sprintf("%04d-W%02d", y, w), start.AddDate(0, 0, 7)
		default: // Monthly
			start := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
			return start.Format("2006-01"), start.AddDate(0, 1, 0)
		}
	}
}

// Rolling returns a Window that approximates a trailing window of length d with
// fixed buckets (floor(now/d)) — the counter-store approximation of a rolling
// window (cf. ratelimit).
func Rolling(d time.Duration) Window {
	return func(_ string, now time.Time) (string, time.Time) {
		bucket := now.Truncate(d)
		return strconv.FormatInt(bucket.Unix(), 10), bucket.Add(d)
	}
}

// Gauge returns a Window that never resets: a live count (seats, storage bytes).
func Gauge() Window {
	return func(_ string, _ time.Time) (string, time.Time) { return "", time.Time{} }
}

// Limit is the caller-resolved cap for a subject. No billing coupling.
type Limit struct {
	Included int64 // allotment included in the plan
	Max      int64 // hard ceiling; usage in (Included, Max] is allowed but billable. Unlimited => no ceiling.
}

// Validate reports whether the Limit is well-formed.
func (l Limit) Validate() error {
	if l.Included < 0 {
		return ErrInvalidLimit
	}
	if l.Max != Unlimited && l.Max < l.Included {
		return ErrInvalidLimit
	}
	return nil
}

// Result reports a quota decision for one subject.
type Result struct {
	Reset     time.Time // when the window rolls (zero for gauges)
	Limit     Limit
	Used      int64 // total consumed this window (post-call)
	Remaining int64 // max(0, Included - Used)
	Overage   int64 // max(0, Used - Included) — the billable signal
	Allowed   bool
}

// makeResult derives the reported fields from a raw used total.
func makeResult(limit Limit, used int64, reset time.Time, allowed bool) Result {
	remaining := max(limit.Included-used, 0)
	overage := max(used-limit.Included, 0)
	return Result{Reset: reset, Limit: limit, Used: used, Remaining: remaining, Overage: overage, Allowed: allowed}
}
