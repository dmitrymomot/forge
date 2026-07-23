package bizcal_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

// oracleCalendar builds the calendar the brute-force oracle test runs
// against: Europe/Kyiv, Mon-Fri 09:00-17:00 business windows plus a Sunday
// 00:00-06:00 window (so the DST transition, which in the EU falls in the
// small hours of the last Sunday of March/October, lands inside an open
// window instead of a closed one), a holiday on a normally-working Monday,
// and a reduced-hours exception on a normally-working Wednesday.
func oracleCalendar(t *testing.T) *bizcal.Calendar {
	t.Helper()
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	weekdayWindows := bizcal.MustWindows("09:00-17:00")
	cal, err := bizcal.New(kyiv,
		bizcal.WithWeekday(time.Monday, weekdayWindows...),
		bizcal.WithWeekday(time.Tuesday, weekdayWindows...),
		bizcal.WithWeekday(time.Wednesday, weekdayWindows...),
		bizcal.WithWeekday(time.Thursday, weekdayWindows...),
		bizcal.WithWeekday(time.Friday, weekdayWindows...),
		bizcal.WithWeekday(time.Sunday, bizcal.MustWindows("00:00-06:00")...),
		// 2026-03-23 is a Monday: a holiday landing on a normally-working day.
		bizcal.WithRule(bizcal.Fixed{Month: time.March, Day: 23}),
		// 2026-03-25 is a Wednesday: reduced hours via CustomDay.
		bizcal.WithExceptions(bizcal.CustomDay(bizcal.MustDate(2026, time.March, 25), bizcal.MustWindows("09:00-11:00")...)),
	)
	if err != nil {
		t.Fatalf("New(oracleCalendar) = %v", err)
	}
	return cal
}

// oracleMinuteTable is a brute-force, minute-granularity ground truth for
// IsOpen over a padded span, built by calling IsOpen directly (which is
// trivially auditable against the fixture by hand) once per minute.
type oracleMinuteTable struct {
	start time.Time
	open  []bool
}

func buildOracleMinuteTable(cal *bizcal.Calendar, start, end time.Time) oracleMinuteTable {
	n := int(end.Sub(start) / time.Minute)
	open := make([]bool, n)
	t := start
	for i := range n {
		open[i] = cal.IsOpen(t)
		t = t.Add(time.Minute)
	}
	return oracleMinuteTable{start: start, open: open}
}

func (o oracleMinuteTable) index(t time.Time) int {
	return int(t.Sub(o.start) / time.Minute)
}

// between sums open minutes in [a,b), mirroring Calendar.Between at
// minute granularity. a and b must be minute-aligned and within table
// bounds.
func (o oracleMinuteTable) between(a, b time.Time) time.Duration {
	ia, ib := o.index(a), o.index(b)
	neg := false
	if ia > ib {
		ia, ib = ib, ia
		neg = true
	}
	var mins int
	for i := ia; i < ib; i++ {
		if o.open[i] {
			mins++
		}
	}
	d := time.Duration(mins) * time.Minute
	if neg {
		return -d
	}
	return d
}

// nextOpen scans forward from t (minute-aligned) for the first open minute,
// mirroring Calendar.NextOpen.
func (o oracleMinuteTable) nextOpen(t time.Time) (time.Time, bool) {
	for i := o.index(t); i < len(o.open); i++ {
		if o.open[i] {
			return o.start.Add(time.Duration(i) * time.Minute), true
		}
	}
	return time.Time{}, false
}

// add consumes budget (a non-negative, whole-minute duration) of open
// minutes starting at t's NextOpen anchor, mirroring Calendar.Add for
// d >= 0.
func (o oracleMinuteTable) add(t time.Time, budget time.Duration) (time.Time, bool) {
	anchor, ok := o.nextOpen(t)
	if !ok {
		return time.Time{}, false
	}
	need := int(budget / time.Minute)
	i := o.index(anchor)
	for consumed := 0; consumed < need; i++ {
		if i >= len(o.open) {
			return time.Time{}, false
		}
		if o.open[i] {
			consumed++
		}
	}
	return o.start.Add(time.Duration(i) * time.Minute), true
}

// TestOracle_MinuteScannerAgreesWithBetweenNextOpenAdd compares Between,
// NextOpen, and Add against a brute-force minute-granularity oracle over a
// 3-week window (2026-03-16 to 2026-04-06) that includes a weekend every
// week, a holiday (2026-03-23), a reduced-hours exception (2026-03-25), and
// the DST spring-forward transition (2026-03-29, Europe/Kyiv).
func TestOracle_MinuteScannerAgreesWithBetweenNextOpenAdd(t *testing.T) {
	cal := oracleCalendar(t)
	loc := cal.Location()

	windowStart := time.Date(2026, time.March, 16, 0, 0, 0, 0, loc)
	windowEnd := time.Date(2026, time.April, 6, 0, 0, 0, 0, loc)
	// Pad the table well past the window so NextOpen/Add scans that reach
	// beyond it (e.g. sampled near windowEnd) still resolve against real
	// ground truth rather than running off the end of the table.
	tablePad := time.Date(2026, time.April, 20, 0, 0, 0, 0, loc)

	oracle := buildOracleMinuteTable(cal, windowStart, tablePad)

	// Deterministic must-hit samples: the holiday, the exception day, a
	// plain weekend day, and instants straddling the DST transition.
	mustHit := []time.Time{
		time.Date(2026, time.March, 23, 10, 0, 0, 0, loc), // holiday (normally working)
		time.Date(2026, time.March, 25, 10, 0, 0, 0, loc), // exception: reduced hours
		time.Date(2026, time.March, 21, 12, 0, 0, 0, loc), // Saturday, closed
		time.Date(2026, time.March, 29, 2, 30, 0, 0, loc), // pre-DST-jump, inside Sunday window
		time.Date(2026, time.March, 29, 5, 0, 0, 0, loc),  // post-DST-jump, inside Sunday window
	}

	rng := rand.New(rand.NewSource(99))
	randMinuteAligned := func() time.Time {
		offset := time.Duration(rng.Intn(int(windowEnd.Sub(windowStart)/time.Minute))) * time.Minute
		return windowStart.Add(offset)
	}

	const samples = 300
	as := make([]time.Time, 0, samples+len(mustHit))
	as = append(as, mustHit...)
	for range samples {
		as = append(as, randMinuteAligned())
	}

	for _, a := range as {
		// Between, paired with a second random/must-hit point.
		b := randMinuteAligned()
		want := oracle.between(a, b)
		if got := cal.Between(a, b); got != want {
			t.Fatalf("Between(%s,%s) = %s, want %s (oracle)", a, b, got, want)
		}

		// NextOpen.
		wantNext, ok := oracle.nextOpen(a)
		gotNext, err := cal.NextOpen(a)
		if !ok {
			// Ran off the padded table; skip rather than assert on a
			// meaningless bound.
			continue
		}
		if err != nil {
			t.Fatalf("NextOpen(%s) errored: %v, want %s (oracle)", a, err, wantNext)
		}
		if !gotNext.Equal(wantNext) {
			t.Fatalf("NextOpen(%s) = %s, want %s (oracle)", a, gotNext, wantNext)
		}

		// Add, with a small whole-minute budget.
		budget := time.Duration(rng.Intn(600)) * time.Minute // up to 10h
		wantAdd, ok := oracle.add(a, budget)
		gotAdd, err := cal.Add(a, budget)
		if !ok {
			continue
		}
		if err != nil {
			t.Fatalf("Add(%s,%s) errored: %v, want %s (oracle)", a, budget, err, wantAdd)
		}
		if !gotAdd.Equal(wantAdd) {
			t.Fatalf("Add(%s,%s) = %s, want %s (oracle)", a, budget, gotAdd, wantAdd)
		}
	}
}
