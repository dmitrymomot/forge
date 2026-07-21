package bizcal

import (
	"iter"
	"slices"
	"time"
)

// IsOpen reports whether the calendar is open at instant t: whether t falls in
// one of t's civil date's open intervals, with the interval start inclusive
// and its end exclusive. A cross-midnight shift is stored split across the two
// civil dates it covers, so instants on either side of the midnight it
// straddles both resolve open.
func (c *Calendar) IsOpen(t time.Time) bool {
	for _, iv := range c.dayIntervals(c.DateOf(t)) {
		if !t.Before(iv.Start) && t.Before(iv.End) {
			return true
		}
	}
	return false
}

// NextOpen returns the earliest open instant at or after t. If t is already
// open it returns t unchanged. The forward scan is bounded by the configured
// horizon (interpreted as a day count of horizon/24h); exhausting it returns
// ErrHorizonExceeded.
func (c *Calendar) NextOpen(t time.Time) (time.Time, error) {
	maxDays := c.horizonDays()
	d := c.DateOf(t)
	for scanned := 0; ; scanned++ {
		if scanned > maxDays {
			return time.Time{}, ErrHorizonExceeded
		}
		for _, iv := range c.dayIntervals(d) {
			if !iv.End.After(t) {
				continue // interval ends at or before t
			}
			if iv.Start.After(t) {
				return iv.Start, nil
			}
			return t, nil // t is inside [start, end)
		}
		d = d.AddDays(1)
	}
}

// Add advances t by d of business time and returns the resulting instant. A
// negative d walks backward. Add(t, 0) equals NextOpen(t). When the budget is
// exhausted exactly at a window edge the result is that boundary instant (the
// closing edge walking forward, the opening edge walking backward), which
// keeps Add a clean inverse of Between. The scan is horizon-bounded and
// returns ErrHorizonExceeded when no reachable instant satisfies the budget.
func (c *Calendar) Add(t time.Time, d time.Duration) (time.Time, error) {
	if d >= 0 {
		anchor, err := c.NextOpen(t)
		if err != nil {
			return time.Time{}, err
		}
		return c.addForward(anchor, d)
	}
	anchor, err := c.prevOpen(t)
	if err != nil {
		return time.Time{}, err
	}
	return c.addBackward(anchor, -d)
}

// Between returns the signed business time in the half-open span [a, b): the
// sum of open-interval durations clipped to the span, measured in absolute
// time so DST transitions are counted exactly. Between(a, a) is zero and
// Between(a, b) equals -Between(b, a).
func (c *Calendar) Between(a, b time.Time) time.Duration {
	if a.Equal(b) {
		return 0
	}
	if a.After(b) {
		return -c.Between(b, a)
	}
	var total time.Duration
	end := c.DateOf(b)
	for day := c.DateOf(a); !day.After(end); day = day.AddDays(1) {
		for _, iv := range c.dayIntervals(day) {
			s, e := iv.Start, iv.End
			if s.Before(a) {
				s = a
			}
			if e.After(b) {
				e = b
			}
			if e.After(s) {
				total += e.Sub(s)
			}
		}
	}
	return total
}

// WindowsBetween yields the calendar's open intervals overlapping [from, to),
// clipped to that span and in ascending order, with intervals adjacent across
// a civil-date boundary (a cross-midnight shift, or consecutive whole-day-open
// days) merged into one. A from at or after to yields nothing.
func (c *Calendar) WindowsBetween(from, to time.Time) iter.Seq[Interval] {
	return func(yield func(Interval) bool) {
		if !from.Before(to) {
			return
		}
		var pend Interval
		var has bool
		flush := func() bool {
			if !has {
				return true
			}
			has = false
			s, e := pend.Start, pend.End
			if s.Before(from) {
				s = from
			}
			if e.After(to) {
				e = to
			}
			if e.After(s) {
				return yield(Interval{Start: s, End: e})
			}
			return true
		}

		end := c.DateOf(to)
		for day := c.DateOf(from); !day.After(end); day = day.AddDays(1) {
			for _, iv := range c.dayIntervals(day) {
				if !iv.End.After(from) {
					continue // fully before the span
				}
				if !iv.Start.Before(to) {
					// The first interval at or after to ends the walk; nothing
					// later can overlap the span.
					if !flush() {
						return
					}
					return
				}
				switch {
				case !has:
					pend, has = iv, true
				case !iv.Start.After(pend.End):
					if iv.End.After(pend.End) {
						pend.End = iv.End
					}
				default:
					if !flush() {
						return
					}
					pend, has = iv, true
				}
			}
		}
		flush()
	}
}

// dayIntervals returns date d's resolved open intervals: sorted, merged, and
// within-day non-adjacent (adjacency only ever occurs across a civil-date
// boundary).
func (c *Calendar) dayIntervals(d Date) []Interval {
	return c.dayPlan(d).intervals
}

// horizonDays converts the configured horizon into a whole-day scan cap,
// matching AddWorkingDays' interpretation.
func (c *Calendar) horizonDays() int {
	return int(c.horizon / (24 * time.Hour))
}

// prevOpen returns the anchor for a backward walk from t: the latest open
// instant at or before t, or the closing boundary of the last interval that
// ends at or before t when t lies in a closed gap. It is the mirror of
// NextOpen and is horizon-bounded.
func (c *Calendar) prevOpen(t time.Time) (time.Time, error) {
	maxDays := c.horizonDays()
	d := c.DateOf(t)
	for scanned := 0; ; scanned++ {
		if scanned > maxDays {
			return time.Time{}, ErrHorizonExceeded
		}
		for _, iv := range slices.Backward(c.dayIntervals(d)) {
			if !iv.Start.Before(t) {
				continue // interval starts at or after t
			}
			if t.Before(iv.End) {
				return t, nil // t is inside [start, end)
			}
			return iv.End, nil // t is in the gap after this interval
		}
		d = d.AddDays(-1)
	}
}

// addForward consumes budget of business time starting at the open instant
// anchor and returns the landing instant, at a closing boundary when the
// budget exhausts exactly at a window end.
func (c *Calendar) addForward(anchor time.Time, budget time.Duration) (time.Time, error) {
	maxDays := c.horizonDays()
	t := anchor
	d := c.DateOf(t)
	for scanned := 0; ; scanned++ {
		if scanned > maxDays {
			return time.Time{}, ErrHorizonExceeded
		}
		for _, iv := range c.dayIntervals(d) {
			if !iv.End.After(t) {
				continue
			}
			start := iv.Start
			if start.Before(t) {
				start = t
			}
			avail := iv.End.Sub(start)
			if budget <= avail {
				return start.Add(budget), nil
			}
			budget -= avail
			t = iv.End
		}
		d = d.AddDays(1)
	}
}

// addBackward consumes budget of business time walking backward from the
// anchor and returns the landing instant, at an opening boundary when the
// budget exhausts exactly at a window start.
func (c *Calendar) addBackward(anchor time.Time, budget time.Duration) (time.Time, error) {
	maxDays := c.horizonDays()
	t := anchor
	d := c.DateOf(t)
	for scanned := 0; ; scanned++ {
		if scanned > maxDays {
			return time.Time{}, ErrHorizonExceeded
		}
		for _, iv := range slices.Backward(c.dayIntervals(d)) {
			if !iv.Start.Before(t) {
				continue
			}
			end := iv.End
			if end.After(t) {
				end = t
			}
			avail := end.Sub(iv.Start)
			if budget <= avail {
				return end.Add(-budget), nil
			}
			budget -= avail
			t = iv.Start
		}
		d = d.AddDays(-1)
	}
}
