package bizcal_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

// nyTime builds an America/New_York wall-clock instant for the calB fixture.
func nyTime(t *testing.T, y int, m time.Month, d, hh, mm int) time.Time {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return time.Date(y, m, d, hh, mm, 0, 0, ny)
}

// --- IsOpen --------------------------------------------------------------

func TestIsOpen(t *testing.T) {
	b := calB(t)
	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"inside morning window", nyTime(t, 2026, time.July, 6, 10, 0), true}, // Mon
		{"exactly at start inclusive", nyTime(t, 2026, time.July, 6, 9, 0), true},
		{"exactly at end exclusive", nyTime(t, 2026, time.July, 6, 13, 0), false},
		{"in lunch gap", nyTime(t, 2026, time.July, 6, 13, 30), false},
		{"inside afternoon window", nyTime(t, 2026, time.July, 6, 15, 0), true},
		{"after close", nyTime(t, 2026, time.July, 6, 18, 0), false},
		{"weekend", nyTime(t, 2026, time.July, 4, 10, 0), false}, // Sat
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.IsOpen(tc.t); got != tc.want {
				t.Fatalf("IsOpen(%s) = %v, want %v", tc.t, got, tc.want)
			}
		})
	}
}

func TestIsOpen_CrossMidnightShift(t *testing.T) {
	c := calC(t)
	// Cross-midnight shift 2026-07-20 22:00 -> 07-21 02:00 UTC: open on both
	// sides of the civil midnight it straddles.
	before := time.Date(2026, time.July, 20, 23, 0, 0, 0, time.UTC)
	after := time.Date(2026, time.July, 21, 1, 0, 0, 0, time.UTC)
	atMidnight := time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC)
	if !c.IsOpen(before) {
		t.Fatal("IsOpen(pre-midnight) = false, want true")
	}
	if !c.IsOpen(after) {
		t.Fatal("IsOpen(post-midnight) = false, want true")
	}
	if !c.IsOpen(atMidnight) {
		t.Fatal("IsOpen(midnight) = false, want true")
	}
	// The shift landing on the holiday is open despite the holiday rule.
	onHoliday := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	if !c.IsOpen(onHoliday) {
		t.Fatal("IsOpen(holiday shift) = false, want true")
	}
}

// --- NextOpen ------------------------------------------------------------

func TestNextOpen(t *testing.T) {
	b := calB(t)
	cases := []struct {
		name string
		from time.Time
		want time.Time
	}{
		{"saturday to monday open", nyTime(t, 2026, time.July, 4, 10, 0), nyTime(t, 2026, time.July, 6, 9, 0)},
		{"mid-lunch to afternoon", nyTime(t, 2026, time.July, 6, 13, 30), nyTime(t, 2026, time.July, 6, 14, 0)},
		{"already open returns itself", nyTime(t, 2026, time.July, 6, 10, 0), nyTime(t, 2026, time.July, 6, 10, 0)},
		{"before open to open", nyTime(t, 2026, time.July, 6, 8, 0), nyTime(t, 2026, time.July, 6, 9, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := b.NextOpen(tc.from)
			if err != nil {
				t.Fatalf("NextOpen(%s) = %v", tc.from, err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("NextOpen(%s) = %s, want %s", tc.from, got, tc.want)
			}
		})
	}
}

func TestNextOpen_AlwaysOpen(t *testing.T) {
	d := calD(t)
	at := time.Date(2026, time.July, 4, 3, 15, 0, 0, time.UTC)
	got, err := d.NextOpen(at)
	if err != nil {
		t.Fatalf("NextOpen = %v", err)
	}
	if !got.Equal(at) {
		t.Fatalf("NextOpen(always-open) = %s, want %s", got, at)
	}
}

// --- Add -----------------------------------------------------------------

func TestAdd_ForwardAcrossWeekend(t *testing.T) {
	b := calB(t)
	// Fri closes 15:00: Fri 14:00 + 1h reaches close, remaining 3h -> Mon
	// 09:00 + 3h = Mon 12:00.
	from := nyTime(t, 2026, time.July, 3, 14, 0)
	want := nyTime(t, 2026, time.July, 6, 12, 0)
	got, err := b.Add(from, 4*time.Hour)
	if err != nil {
		t.Fatalf("Add = %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("Add(Fri 14:00, 4h) = %s, want %s", got, want)
	}
}

func TestAdd_LandsAtClosingBoundary(t *testing.T) {
	b := calB(t)
	// Budget exhausting exactly at a window end lands on the closing boundary
	// instant, not the next window start.
	from := nyTime(t, 2026, time.July, 6, 9, 0)
	want := nyTime(t, 2026, time.July, 6, 13, 0)
	got, err := b.Add(from, 4*time.Hour)
	if err != nil {
		t.Fatalf("Add = %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("Add(Mon 09:00, 4h) = %s, want %s (closing boundary)", got, want)
	}
	// Round-trips with Between.
	if d := b.Between(from, got); d != 4*time.Hour {
		t.Fatalf("Between(Mon 09:00, Mon 13:00) = %s, want 4h", d)
	}
}

func TestAdd_Backward(t *testing.T) {
	b := calB(t)
	// Mirror of the forward weekend case: Mon 12:00 - 4h -> Fri 14:00.
	from := nyTime(t, 2026, time.July, 6, 12, 0)
	want := nyTime(t, 2026, time.July, 3, 14, 0)
	got, err := b.Add(from, -4*time.Hour)
	if err != nil {
		t.Fatalf("Add = %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("Add(Mon 12:00, -4h) = %s, want %s", got, want)
	}
}

func TestAdd_ZeroEqualsNextOpen(t *testing.T) {
	b := calB(t)
	from := nyTime(t, 2026, time.July, 6, 13, 30) // lunch gap
	add0, err := b.Add(from, 0)
	if err != nil {
		t.Fatalf("Add(_,0) = %v", err)
	}
	next, err := b.NextOpen(from)
	if err != nil {
		t.Fatalf("NextOpen = %v", err)
	}
	if !add0.Equal(next) {
		t.Fatalf("Add(t,0) = %s, want NextOpen = %s", add0, next)
	}
}

func TestAdd_AlwaysOpenIsPlainDuration(t *testing.T) {
	d := calD(t)
	at := time.Date(2026, time.July, 4, 20, 0, 0, 0, time.UTC)
	got, err := d.Add(at, 8*time.Hour)
	if err != nil {
		t.Fatalf("Add = %v", err)
	}
	if !got.Equal(at.Add(8 * time.Hour)) {
		t.Fatalf("Add(always-open, 8h) = %s, want %s", got, at.Add(8*time.Hour))
	}
}

// --- Between -------------------------------------------------------------

func TestBetween_SignedSymmetry(t *testing.T) {
	b := calB(t)
	a := nyTime(t, 2026, time.July, 6, 10, 0)
	z := nyTime(t, 2026, time.July, 6, 16, 0)
	fwd := b.Between(a, z)
	rev := b.Between(z, a)
	if fwd != -rev {
		t.Fatalf("Between not signed-symmetric: fwd=%s rev=%s", fwd, rev)
	}
	// [10:00,16:00): 10-13 (3h) + 14-16 (2h) = 5h.
	if fwd != 5*time.Hour {
		t.Fatalf("Between(10:00,16:00) = %s, want 5h", fwd)
	}
	if b.Between(a, a) != 0 {
		t.Fatalf("Between(a,a) = %s, want 0", b.Between(a, a))
	}
}

func TestBetween_ClosedWeekendIsZero(t *testing.T) {
	b := calB(t)
	sat := nyTime(t, 2026, time.July, 4, 0, 0)
	mon := nyTime(t, 2026, time.July, 6, 0, 0)
	if got := b.Between(sat, mon); got != 0 {
		t.Fatalf("Between(Sat,Mon) = %s, want 0", got)
	}
}

func TestBetween_WorkdaysWallSpan(t *testing.T) {
	a := calA(t)
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	// Whole-day-open workdays model: business time equals wall span within a
	// single working day. 2026-07-21 is a Tuesday.
	from := time.Date(2026, time.July, 21, 9, 12, 0, 0, kyiv)
	to := time.Date(2026, time.July, 21, 16, 5, 0, 0, kyiv)
	if got := a.Between(from, to); got != 6*time.Hour+53*time.Minute {
		t.Fatalf("Between(Tue 09:12, Tue 16:05) = %s, want 6h53m", got)
	}
	// Spanning a weekend counts only the weekday wall time: Fri 07-17 full day
	// (8h capacity but whole-day-open => 24h wall) is not what we assert here;
	// instead assert the weekend itself contributes nothing.
	satMid := time.Date(2026, time.July, 25, 0, 0, 0, 0, kyiv)
	monMid := time.Date(2026, time.July, 27, 0, 0, 0, 0, kyiv)
	if got := a.Between(satMid, monMid); got != 0 {
		t.Fatalf("Between(Sat,Mon) = %s, want 0", got)
	}
}

// --- DST correctness -----------------------------------------------------

func kyivSundayCal(t *testing.T) *bizcal.Calendar {
	t.Helper()
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	cal, err := bizcal.New(kyiv, bizcal.WithWeekday(time.Sunday, bizcal.MustWindows("00:00-06:00")...))
	if err != nil {
		t.Fatalf("New(kyivSunday) = %v", err)
	}
	return cal
}

func TestBetween_DST(t *testing.T) {
	cal := kyivSundayCal(t)
	kyiv := cal.Location()
	// Spring-forward 2026-03-29: 03:00 -> 04:00 skip, so the 00:00-06:00 wall
	// window is only 5 absolute hours.
	springFrom := time.Date(2026, time.March, 29, 0, 0, 0, 0, kyiv)
	springTo := time.Date(2026, time.March, 29, 6, 0, 0, 0, kyiv)
	if got := cal.Between(springFrom, springTo); got != 5*time.Hour {
		t.Fatalf("Between(spring-forward wall day) = %s, want 5h", got)
	}
	// Fall-back 2026-10-25: 04:00 -> 03:00 repeat, so the window is 7 hours.
	fallFrom := time.Date(2026, time.October, 25, 0, 0, 0, 0, kyiv)
	fallTo := time.Date(2026, time.October, 25, 6, 0, 0, 0, kyiv)
	if got := cal.Between(fallFrom, fallTo); got != 7*time.Hour {
		t.Fatalf("Between(fall-back wall day) = %s, want 7h", got)
	}
}

func TestAdd_DSTDurationExact(t *testing.T) {
	cal := kyivSundayCal(t)
	kyiv := cal.Location()
	// SLA add across spring-forward stays absolute-duration exact: 4 in-window
	// hours from 01:00 land at the 06:00 close, 4 absolute hours later.
	from := time.Date(2026, time.March, 29, 1, 0, 0, 0, kyiv)
	got, err := cal.Add(from, 4*time.Hour)
	if err != nil {
		t.Fatalf("Add = %v", err)
	}
	if got.Sub(from) != 4*time.Hour {
		t.Fatalf("Add across spring-forward: elapsed %s, want 4h absolute", got.Sub(from))
	}
	want := time.Date(2026, time.March, 29, 6, 0, 0, 0, kyiv)
	if !got.Equal(want) {
		t.Fatalf("Add(01:00, 4h) = %s, want %s", got, want)
	}
}

// --- WindowsBetween ------------------------------------------------------

func collect(seq func(func(bizcal.Interval) bool)) []bizcal.Interval {
	var out []bizcal.Interval
	seq(func(iv bizcal.Interval) bool {
		out = append(out, iv)
		return true
	})
	return out
}

func TestWindowsBetween_Windows(t *testing.T) {
	b := calB(t)
	// A single Monday: two split windows, both fully inside the range.
	from := nyTime(t, 2026, time.July, 6, 0, 0)
	to := nyTime(t, 2026, time.July, 7, 0, 0)
	got := collect(b.WindowsBetween(from, to))
	if len(got) != 2 {
		t.Fatalf("WindowsBetween(Mon) yielded %d intervals, want 2", len(got))
	}
	if !got[0].Start.Equal(nyTime(t, 2026, time.July, 6, 9, 0)) || !got[0].End.Equal(nyTime(t, 2026, time.July, 6, 13, 0)) {
		t.Fatalf("first interval = %v", got[0])
	}
	if !got[1].Start.Equal(nyTime(t, 2026, time.July, 6, 14, 0)) || !got[1].End.Equal(nyTime(t, 2026, time.July, 6, 18, 0)) {
		t.Fatalf("second interval = %v", got[1])
	}
}

func TestWindowsBetween_ClipsBothEdges(t *testing.T) {
	b := calB(t)
	// Clip inside the morning window at the from edge and inside the afternoon
	// window at the to edge.
	from := nyTime(t, 2026, time.July, 6, 10, 0)
	to := nyTime(t, 2026, time.July, 6, 15, 0)
	got := collect(b.WindowsBetween(from, to))
	if len(got) != 2 {
		t.Fatalf("WindowsBetween(clipped) yielded %d, want 2", len(got))
	}
	if !got[0].Start.Equal(from) {
		t.Fatalf("first clipped start = %s, want %s", got[0].Start, from)
	}
	if !got[1].End.Equal(to) {
		t.Fatalf("last clipped end = %s, want %s", got[1].End, to)
	}
}

func TestWindowsBetween_MergesCrossMidnightShift(t *testing.T) {
	c := calC(t)
	// The 22:00 -> 02:00 shift is split across two civil dates internally but
	// must emerge as one merged interval.
	from := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC)
	got := collect(c.WindowsBetween(from, to))
	if len(got) != 1 {
		t.Fatalf("WindowsBetween(shift) yielded %d, want 1 merged", len(got))
	}
	wantStart := time.Date(2026, time.July, 20, 22, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, time.July, 21, 2, 0, 0, 0, time.UTC)
	if !got[0].Start.Equal(wantStart) || !got[0].End.Equal(wantEnd) {
		t.Fatalf("merged shift = %v, want [%s,%s)", got[0], wantStart, wantEnd)
	}
}

func TestWindowsBetween_AlwaysOpenSingleInterval(t *testing.T) {
	d := calD(t)
	from := time.Date(2026, time.July, 4, 6, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.July, 9, 18, 0, 0, 0, time.UTC)
	got := collect(d.WindowsBetween(from, to))
	if len(got) != 1 {
		t.Fatalf("WindowsBetween(always-open) yielded %d, want 1", len(got))
	}
	if !got[0].Start.Equal(from) || !got[0].End.Equal(to) {
		t.Fatalf("always-open merged = %v, want [%s,%s)", got[0], from, to)
	}
}

func TestWindowsBetween_EmptyWhenInverted(t *testing.T) {
	b := calB(t)
	from := nyTime(t, 2026, time.July, 6, 12, 0)
	to := nyTime(t, 2026, time.July, 6, 9, 0)
	if got := collect(b.WindowsBetween(from, to)); len(got) != 0 {
		t.Fatalf("WindowsBetween(from>=to) yielded %d, want 0", len(got))
	}
}

// --- Horizon -------------------------------------------------------------

// alwaysClosedCal builds a calendar with a base but a rule that closes every
// day of every year, so no open instant is ever reachable.
func alwaysClosedCal(t *testing.T, horizon time.Duration) *bizcal.Calendar {
	t.Helper()
	allDays := bizcal.RuleFunc(func(year int) []bizcal.Date {
		var ds []bizcal.Date
		d := bizcal.MustDate(year, time.January, 1)
		end := bizcal.MustDate(year+1, time.January, 1)
		for d.Before(end) {
			ds = append(ds, d)
			d = d.AddDays(1)
		}
		return ds
	})
	cal, err := bizcal.New(time.UTC,
		bizcal.WithWorkdays(8*time.Hour, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday),
		bizcal.WithRule(allDays),
		bizcal.WithHorizon(horizon),
	)
	if err != nil {
		t.Fatalf("New(alwaysClosed) = %v", err)
	}
	return cal
}

func TestHorizonExceeded(t *testing.T) {
	cal := alwaysClosedCal(t, 48*time.Hour)
	at := time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
	if _, err := cal.NextOpen(at); !errors.Is(err, bizcal.ErrHorizonExceeded) {
		t.Fatalf("NextOpen = %v, want ErrHorizonExceeded", err)
	}
	if _, err := cal.Add(at, time.Hour); !errors.Is(err, bizcal.ErrHorizonExceeded) {
		t.Fatalf("Add(forward) = %v, want ErrHorizonExceeded", err)
	}
	if _, err := cal.Add(at, -time.Hour); !errors.Is(err, bizcal.ErrHorizonExceeded) {
		t.Fatalf("Add(backward) = %v, want ErrHorizonExceeded", err)
	}
}
