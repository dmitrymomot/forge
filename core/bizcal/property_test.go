package bizcal_test

import (
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

// propIterations is the number of seeded pseudo-random cases each property
// test runs. The generator is seeded from a fixed constant (never time or
// flags) so failures are deterministic and reproducible across runs.
const propIterations = 500

// randInstant returns a uniformly random instant within calendar year 2026
// in loc. Full-nanosecond precision is fine here: the algebraic properties
// under test hold for any pair of instants, not just minute-aligned ones.
func randInstant(rng *rand.Rand, loc *time.Location) time.Time {
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, loc)
	end := time.Date(2027, time.January, 1, 0, 0, 0, 0, loc)
	span := int64(end.Sub(start))
	return start.Add(time.Duration(rng.Int63n(span)))
}

// randDate2026 returns a uniformly random civil date within 2026.
func randDate2026(rng *rand.Rand) bizcal.Date {
	start := bizcal.MustDate(2026, time.January, 1)
	return start.AddDays(rng.Intn(365))
}

// propertyFixtures returns the three fixture calendars the brief calls out
// for property coverage: (a) the workdays model, (b) the weekly-windows
// model, and (c) the shifts-only model.
func propertyFixtures(t *testing.T) []*bizcal.Calendar {
	t.Helper()
	return []*bizcal.Calendar{calA(t), calB(t), calC(t)}
}

// --- Between: signed symmetry and additivity -----------------------------

func TestProperty_BetweenSignedSymmetry(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	cals := propertyFixtures(t)
	for i := range propIterations {
		cal := cals[i%len(cals)]
		a := randInstant(rng, cal.Location())
		b := randInstant(rng, cal.Location())
		fwd := cal.Between(a, b)
		rev := cal.Between(b, a)
		if fwd != -rev {
			t.Fatalf("iter %d: Between(a,b) != -Between(b,a): a=%s b=%s fwd=%s rev=%s", i, a, b, fwd, rev)
		}
	}
}

func TestProperty_BetweenAdditive(t *testing.T) {
	rng := rand.New(rand.NewSource(43))
	cals := propertyFixtures(t)
	for i := range propIterations {
		cal := cals[i%len(cals)]
		pts := []time.Time{randInstant(rng, cal.Location()), randInstant(rng, cal.Location()), randInstant(rng, cal.Location())}
		sort.Slice(pts, func(x, y int) bool { return pts[x].Before(pts[y]) })
		a, b, c := pts[0], pts[1], pts[2]
		lhs := cal.Between(a, c)
		rhs := cal.Between(a, b) + cal.Between(b, c)
		if lhs != rhs {
			t.Fatalf("iter %d: Between(a,c) != Between(a,b)+Between(b,c): a=%s b=%s c=%s lhs=%s rhs=%s", i, a, b, c, lhs, rhs)
		}
	}
}

// --- Add: round-trips through Between; NextOpen: idempotent --------------

func TestProperty_AddRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(44))
	cals := propertyFixtures(t)
	for i := range propIterations {
		cal := cals[i%len(cals)]
		start := randInstant(rng, cal.Location())
		d := time.Duration(rng.Int63n(int64(30 * 24 * time.Hour))) // always >= 0

		result, err := cal.Add(start, d)
		if err != nil {
			continue // horizon exhausted on a sparse fixture is not a violation
		}
		anchor, err := cal.NextOpen(start)
		if err != nil {
			t.Fatalf("iter %d: NextOpen(start) errored after Add succeeded: %v", i, err)
		}
		if got := cal.Between(anchor, result); got != d {
			t.Fatalf("iter %d: Add round-trip broke: Between(NextOpen(start),result)=%s, want %s (start=%s)", i, got, d, start)
		}
	}
}

func TestProperty_NextOpenIdempotent(t *testing.T) {
	rng := rand.New(rand.NewSource(45))
	cals := []*bizcal.Calendar{calA(t), calB(t), calC(t), calD(t)}
	for i := range propIterations {
		cal := cals[i%len(cals)]
		t0 := randInstant(rng, cal.Location())
		got1, err1 := cal.NextOpen(t0)
		if err1 != nil {
			continue
		}
		got2, err2 := cal.NextOpen(got1)
		if err2 != nil {
			t.Fatalf("iter %d: NextOpen(NextOpen(t)) errored: %v", i, err2)
		}
		if !got2.Equal(got1) {
			t.Fatalf("iter %d: NextOpen not idempotent: t=%s got1=%s got2=%s", i, t0, got1, got2)
		}
	}
}

// --- WorkingDays / ScheduledBetween: aggregate == per-day loop -----------

func TestProperty_WorkingDaysMatchesLoop(t *testing.T) {
	rng := rand.New(rand.NewSource(46))
	cals := propertyFixtures(t)
	for i := range propIterations {
		cal := cals[i%len(cals)]
		from, to := randDate2026(rng), randDate2026(rng)
		if to.Before(from) {
			from, to = to, from
		}
		want := 0
		for d := from; d.Before(to); d = d.AddDays(1) {
			if cal.IsWorkingDay(d) {
				want++
			}
		}
		if got := cal.WorkingDays(from, to); got != want {
			t.Fatalf("iter %d: WorkingDays(%s,%s)=%d, want %d (loop count)", i, from, to, got, want)
		}
	}
}

func TestProperty_ScheduledBetweenMatchesSum(t *testing.T) {
	rng := rand.New(rand.NewSource(47))
	cals := propertyFixtures(t)
	for i := range propIterations {
		cal := cals[i%len(cals)]
		from, to := randDate2026(rng), randDate2026(rng)
		if to.Before(from) {
			from, to = to, from
		}
		var want time.Duration
		for d := from; d.Before(to); d = d.AddDays(1) {
			want += cal.DayDuration(d)
		}
		if got := cal.ScheduledBetween(from, to); got != want {
			t.Fatalf("iter %d: ScheduledBetween(%s,%s)=%s, want %s (sum of DayDuration)", i, from, to, got, want)
		}
	}
}

// --- Pinned tests (controller-directed, Task 4/5 review follow-ups) ------

// TestPin_ShiftOverlappingBaseWindow_CapacityAdditiveIntervalsUnion pins the
// current additive-capacity / union-intervals semantic for a shift rostered
// on top of a base window it overlaps: DayDuration double-counts the
// overlap (base + full shift duration) while the open intervals themselves
// merge into a single continuous span.
func TestPin_ShiftOverlappingBaseWindow_CapacityAdditiveIntervalsUnion(t *testing.T) {
	// 2026-07-06 is a Monday.
	mon := bizcal.MustDate(2026, time.July, 6)
	shiftStart := time.Date(2026, time.July, 6, 10, 0, 0, 0, time.UTC)
	shiftEnd := time.Date(2026, time.July, 6, 12, 0, 0, 0, time.UTC)
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWeekday(time.Monday, bizcal.MustWindows("09:00-17:00")...),
		bizcal.WithShifts(bizcal.Shift(shiftStart, shiftEnd)),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	// Capacity double-counts: 8h base + 2h shift = 10h, even though the shift
	// is fully contained within the base window.
	if got := cal.DayDuration(mon); got != 10*time.Hour {
		t.Fatalf("DayDuration(overlap) = %s, want 10h (additive double-count)", got)
	}

	// Open intervals are the union: exactly one continuous [09:00,17:00)
	// interval, not two.
	dayStart := time.Date(2026, time.July, 6, 0, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, time.July, 7, 0, 0, 0, 0, time.UTC)
	got := collect(cal.WindowsBetween(dayStart, dayEnd))
	if len(got) != 1 {
		t.Fatalf("WindowsBetween(overlap day) yielded %d intervals, want 1 merged", len(got))
	}
	wantStart := time.Date(2026, time.July, 6, 9, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, time.July, 6, 17, 0, 0, 0, time.UTC)
	if !got[0].Start.Equal(wantStart) || !got[0].End.Equal(wantEnd) {
		t.Fatalf("merged interval = %v, want [%s,%s)", got[0], wantStart, wantEnd)
	}
}

// TestPin_ShiftAcrossYearBoundary_SplitsAcrossBothYearsPlans pins that a
// shift straddling Dec 31 -> Jan 1 contributes correctly to both years'
// independently-built, independently-cached yearPlans.
func TestPin_ShiftAcrossYearBoundary_SplitsAcrossBothYearsPlans(t *testing.T) {
	start := time.Date(2026, time.December, 31, 23, 0, 0, 0, time.UTC)
	end := time.Date(2027, time.January, 1, 2, 0, 0, 0, time.UTC)
	cal, err := bizcal.New(time.UTC, bizcal.WithShifts(bizcal.Shift(start, end)))
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	if got := cal.DayDuration(bizcal.MustDate(2026, time.December, 31)); got != time.Hour {
		t.Fatalf("DayDuration(2026-12-31) = %s, want 1h", got)
	}
	if got := cal.DayDuration(bizcal.MustDate(2027, time.January, 1)); got != 2*time.Hour {
		t.Fatalf("DayDuration(2027-01-01) = %s, want 2h", got)
	}
}

// TestPin_CustomDayException_ObservedViaDayDurationAndIsWorkingDay pins the
// windows-model CustomDay override as visible through the public day-op
// surface, not just construction.
func TestPin_CustomDayException_ObservedViaDayDurationAndIsWorkingDay(t *testing.T) {
	// 2026-07-06 is a Monday.
	mon := bizcal.MustDate(2026, time.July, 6)
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWeekday(time.Monday, bizcal.MustWindows("09:00-17:00")...),
		bizcal.WithExceptions(bizcal.CustomDay(mon, bizcal.MustWindows("10:00-11:00")...)),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if got := cal.DayDuration(mon); got != time.Hour {
		t.Fatalf("DayDuration(CustomDay) = %s, want 1h", got)
	}
	if !cal.IsWorkingDay(mon) {
		t.Fatal("IsWorkingDay(CustomDay) = false, want true")
	}
}

// TestPin_ZeroCapacityShortDay_IsNonWorkingDay pins that a ShortDay
// exception with zero capacity reads as a non-working day, not merely as a
// degenerate-but-open one.
func TestPin_ZeroCapacityShortDay_IsNonWorkingDay(t *testing.T) {
	// 2026-07-06 is a Monday.
	mon := bizcal.MustDate(2026, time.July, 6)
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWorkdays(8*time.Hour, time.Monday),
		bizcal.WithExceptions(bizcal.ShortDay(mon, 0)),
	)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if got := cal.DayDuration(mon); got != 0 {
		t.Fatalf("DayDuration(zero ShortDay) = %s, want 0", got)
	}
	if cal.IsWorkingDay(mon) {
		t.Fatal("IsWorkingDay(zero ShortDay) = true, want false")
	}
}

// TestPin_AlwaysOpenDST_CapacityIsRealCivilDaySpan pins that an always-open
// calendar's DayDuration on a DST-transition day is the real civil-day span
// (23h spring-forward, 25h fall-back), not a nominal 24h.
func TestPin_AlwaysOpenDST_CapacityIsRealCivilDaySpan(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	cal, err := bizcal.New(ny, bizcal.WithAlwaysOpen())
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	// 2026-03-08: US spring-forward (2nd Sunday of March).
	if got := cal.DayDuration(bizcal.MustDate(2026, time.March, 8)); got != 23*time.Hour {
		t.Fatalf("DayDuration(spring-forward) = %s, want 23h", got)
	}
	// 2026-11-01: US fall-back (1st Sunday of November).
	if got := cal.DayDuration(bizcal.MustDate(2026, time.November, 1)); got != 25*time.Hour {
		t.Fatalf("DayDuration(fall-back) = %s, want 25h", got)
	}
	// A non-transition day stays a nominal 24h.
	if got := cal.DayDuration(bizcal.MustDate(2026, time.March, 9)); got != 24*time.Hour {
		t.Fatalf("DayDuration(normal day) = %s, want 24h", got)
	}
}
