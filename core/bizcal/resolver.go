package bizcal

import "time"

// dayPlan is a single civil date's resolved schedule: its open intervals (in
// absolute time) and its scheduled capacity. For the windows and always-open
// models capacity equals the summed interval durations; for the workdays
// model the whole civil day is open but capacity is the fixed per-day value,
// so the two intentionally differ. Rostered shift time adds to both.
type dayPlan struct {
	intervals []Interval
	capacity  time.Duration
}

// yearPlan memoizes every civil date's dayPlan for one year, computed lazily
// on first touch of that year and cached on the Calendar. days is indexed by
// day-of-year minus one (dayOfYear returns 1-366) rather than keyed by Date,
// so a resolved lookup is a slice index instead of a map hash+probe.
type yearPlan struct {
	days []dayPlan
}

// dayPlan resolves d's plan, building and caching d's year on first touch.
func (c *Calendar) dayPlan(d Date) dayPlan {
	yp := c.yearPlanFor(d.Year)
	idx := dayOfYear(d) - 1
	if idx < 0 || idx >= len(yp.days) {
		return dayPlan{}
	}
	return yp.days[idx]
}

// yearPlanFor returns the memoized plan for year, building it under the write
// lock if absent. Reads take the read lock so concurrent day ops on already
// built years never serialize.
func (c *Calendar) yearPlanFor(year int) *yearPlan {
	c.mu.RLock()
	yp := c.years[year]
	c.mu.RUnlock()
	if yp != nil {
		return yp
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if yp = c.years[year]; yp != nil {
		return yp
	}
	yp = c.buildYear(year)
	c.years[year] = yp
	return yp
}

// buildYear resolves every civil date in year through the full layering
// pipeline (base, holiday rules, exceptions, shifts) once.
func (c *Calendar) buildYear(year int) *yearPlan {
	ruleDates := c.ruleDates(year)
	buckets := c.shiftBuckets(year)

	n := 365
	if isLeapYear(year) {
		n = 366
	}
	days := make([]dayPlan, n)
	d := Date{Year: year, Month: time.January, Day: 1}
	for i := range days {
		days[i] = c.resolvePlan(d, ruleDates, buckets)
		d = d.AddDays(1)
	}
	return &yearPlan{days: days}
}

// cumulativeDaysBeforeMonth[m] is the count of days in a non-leap year
// falling in months before m (1-indexed; index 0 is unused).
var cumulativeDaysBeforeMonth = [...]int{0, 0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}

// dayOfYear returns d's 1-based ordinal day within its year (Jan 1 is 1,
// Dec 31 is 365 or 366) via pure arithmetic, with no time.Date call.
func dayOfYear(d Date) int {
	doy := cumulativeDaysBeforeMonth[d.Month] + d.Day
	if d.Month > time.February && isLeapYear(d.Year) {
		doy++
	}
	return doy
}

// isLeapYear reports whether year is a Gregorian leap year.
func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// ruleDates expands every registered rule for year, keeping only dates that
// actually fall in year (rules that return out-of-year dates are ignored).
func (c *Calendar) ruleDates(year int) map[Date]bool {
	if len(c.rules) == 0 {
		return nil
	}
	out := make(map[Date]bool)
	for _, r := range c.rules {
		for _, rd := range r.Dates(year) {
			if rd.Year == year {
				out[rd] = true
			}
		}
	}
	return out
}

// resolvePlan layers the four schedule sources for a single date: base ->
// holiday rule (full day off) -> exception (replaces the day) -> shifts (add
// open time and capacity on top, winning over any day-off).
func (c *Calendar) resolvePlan(d Date, ruleDates map[Date]bool, buckets map[Date][]Interval) dayPlan {
	p := c.basePlan(d)
	if ruleDates[d] {
		p = dayPlan{}
	}
	if exc, ok := c.exceptions[d]; ok {
		p = c.exceptionPlan(d, exc)
	}
	if segs := buckets[d]; len(segs) > 0 {
		p.intervals = mergeIntervals(append(p.intervals, segs...))
		for _, iv := range segs {
			p.capacity += iv.Duration()
		}
	}
	return p
}

// basePlan resolves d against the calendar's base source alone.
func (c *Calendar) basePlan(d Date) dayPlan {
	switch {
	case c.usedWorkdays:
		wd := d.Weekday()
		if !c.workdaySet[wd] {
			return dayPlan{}
		}
		start, next := c.civilBounds(d)
		return dayPlan{intervals: []Interval{{Start: start, End: next}}, capacity: c.workdayCap[wd]}
	case c.usedAlwaysOpen:
		start, next := c.civilBounds(d)
		return dayPlan{intervals: []Interval{{Start: start, End: next}}, capacity: next.Sub(start)}
	case c.usedWeekday:
		return c.windowsPlan(d, c.weekly[d.Weekday()])
	default:
		return dayPlan{}
	}
}

// exceptionPlan resolves d's exception override.
func (c *Calendar) exceptionPlan(d Date, exc Exception) dayPlan {
	switch exc.kind {
	case exceptionShortDay:
		if exc.capacity == 0 {
			return dayPlan{}
		}
		start, next := c.civilBounds(d)
		return dayPlan{intervals: []Interval{{Start: start, End: next}}, capacity: exc.capacity}
	case exceptionCustomDay:
		return c.windowsPlan(d, exc.windows)
	default: // exceptionDayOff
		return dayPlan{}
	}
}

// windowsPlan expands wall-clock windows into absolute intervals on d,
// summing their real (DST-adjusted) durations into the capacity.
func (c *Calendar) windowsPlan(d Date, ws []Window) dayPlan {
	if len(ws) == 0 {
		return dayPlan{}
	}
	intervals := make([]Interval, 0, len(ws))
	var capacity time.Duration
	for _, w := range ws {
		iv := c.windowInterval(d, w)
		intervals = append(intervals, iv)
		capacity += iv.Duration()
	}
	return dayPlan{intervals: intervals, capacity: capacity}
}

// windowInterval resolves window w on date d into an absolute interval in the
// calendar's zone. A 24:00 end normalizes to the next civil midnight, and DST
// gaps or repeats are handled by time.Date normalization.
func (c *Calendar) windowInterval(d Date, w Window) Interval {
	sh, sm := w.Start()
	eh, em := w.End()
	start := time.Date(d.Year, d.Month, d.Day, sh, sm, 0, 0, c.loc)
	end := time.Date(d.Year, d.Month, d.Day, eh, em, 0, 0, c.loc)
	return Interval{Start: start, End: end}
}

// civilBounds returns the absolute start (00:00) and exclusive end (next
// civil midnight) of date d in the calendar's zone. On a DST transition day
// the span is 23h or 25h rather than 24h.
func (c *Calendar) civilBounds(d Date) (start, next time.Time) {
	start = time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, c.loc)
	next = time.Date(d.Year, d.Month, d.Day+1, 0, 0, 0, 0, c.loc)
	return start, next
}

// shiftBuckets clips every shift to the civil dates it covers within year and
// buckets the resulting segments by date. Segments falling outside year are
// dropped, so a shift straddling a year boundary contributes only its
// in-year portion.
func (c *Calendar) shiftBuckets(year int) map[Date][]Interval {
	if len(c.shifts) == 0 {
		return nil
	}
	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, c.loc)
	yearEnd := time.Date(year+1, time.January, 1, 0, 0, 0, 0, c.loc)

	buckets := make(map[Date][]Interval)
	for _, sh := range c.shifts {
		if !sh.End.After(yearStart) || !sh.Start.Before(yearEnd) {
			continue
		}
		for cur := c.DateOf(sh.Start); ; cur = cur.AddDays(1) {
			dayStart, dayNext := c.civilBounds(cur)
			if !dayStart.Before(sh.End) {
				break
			}
			segStart := maxTime(dayStart, sh.Start)
			segEnd := minTime(dayNext, sh.End)
			if cur.Year == year && segEnd.After(segStart) {
				buckets[cur] = append(buckets[cur], Interval{Start: segStart, End: segEnd})
			}
			if !dayNext.Before(sh.End) {
				break
			}
		}
	}
	return buckets
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
