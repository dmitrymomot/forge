package bizcal_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

// maxFuzzOffsetSec bounds Between/Add fuzz offsets so a pathological large
// int64 doesn't turn the day-by-day walk inside Between/Add into a
// multi-million-iteration scan; 90 days is generous headroom over the
// property tests' 30-day budget while staying fast under -fuzz.
const maxFuzzOffsetSec = 90 * 24 * 3600

// boundedOffset folds an arbitrary int64 into [0, maxFuzzOffsetSec).
func boundedOffset(v int64) int64 {
	if v < 0 {
		v = -v
	}
	return v % maxFuzzOffsetSec
}

// FuzzParseWindow checks that ParseWindow never panics on arbitrary input,
// and that every value it successfully parses round-trips through String:
// re-parsing the formatted output yields an equal Window.
func FuzzParseWindow(f *testing.F) {
	seeds := []string{
		"09:00-17:30",
		"9:00-17:00",
		"00:00-24:00",
		"",
		"09:00",
		"17:00-09:00",
		"09:60-10:00",
		"09:00-25:00",
		"garbage",
		"09:00-17:00-extra",
		"-17:00",
		"09-17",
		"24:00-24:00",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		w, err := bizcal.ParseWindow(s)
		if err != nil {
			return
		}
		w2, err := bizcal.ParseWindow(w.String())
		if err != nil {
			t.Fatalf("ParseWindow(%q) = %v, err=nil; but re-parsing its String() %q failed: %v", s, w, w.String(), err)
		}
		if w2 != w {
			t.Fatalf("ParseWindow(%q) = %v; round-trip through String() %q gave %v", s, w, w.String(), w2)
		}
	})
}

// FuzzBetweenSymmetry fuzzes Between on fixture (b) with a random base
// instant and two forward offsets, checking signed symmetry, additivity
// across the midpoint, and that business time never exceeds the wall span.
func FuzzBetweenSymmetry(f *testing.F) {
	// DST instants for fixture (b)'s zone (America/New_York): 2026 spring
	// forward (2nd Sunday of March) and fall back (1st Sunday of November).
	spring := time.Date(2026, time.March, 8, 1, 30, 0, 0, time.UTC).Unix()
	fall := time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC).Unix()
	f.Add(spring, int64(3600), int64(7200))
	f.Add(fall, int64(3600), int64(7200))
	f.Add(int64(1751500800), int64(0), int64(0))

	f.Fuzz(func(t *testing.T, unixSec int64, off1, off2 int64) {
		b := calB(t)
		loc := b.Location()
		a := time.Unix(unixSec, 0).In(loc)
		mid := a.Add(time.Duration(boundedOffset(off1)) * time.Second)
		z := mid.Add(time.Duration(boundedOffset(off2)) * time.Second)

		fwd := b.Between(a, z)
		rev := b.Between(z, a)
		if fwd != -rev {
			t.Fatalf("Between not signed-symmetric: a=%s z=%s fwd=%s rev=%s", a, z, fwd, rev)
		}

		if sum := b.Between(a, mid) + b.Between(mid, z); sum != fwd {
			t.Fatalf("Between not additive: a=%s mid=%s z=%s Between(a,z)=%s sum=%s", a, mid, z, fwd, sum)
		}

		wall := z.Sub(a)
		if fwd < 0 || fwd > wall {
			t.Fatalf("Between(a,z)=%s exceeds wall span %s: a=%s z=%s", fwd, wall, a, z)
		}
	})
}

// FuzzAddRoundTrip fuzzes Add on fixture (b) with a random start instant and
// a random non-negative budget, checking that whenever Add succeeds its
// result round-trips through Between against NextOpen(start).
func FuzzAddRoundTrip(f *testing.F) {
	spring := time.Date(2026, time.March, 8, 1, 30, 0, 0, time.UTC).Unix()
	fall := time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC).Unix()
	f.Add(spring, int64(3600))
	f.Add(fall, int64(3600))
	f.Add(int64(1751500800), int64(0))

	f.Fuzz(func(t *testing.T, unixSec int64, budgetSec int64) {
		b := calB(t)
		loc := b.Location()
		start := time.Unix(unixSec, 0).In(loc)
		d := time.Duration(boundedOffset(budgetSec)) * time.Second

		result, err := b.Add(start, d)
		if err != nil {
			return // horizon exceeded is not a fuzz violation
		}
		anchor, err := b.NextOpen(start)
		if err != nil {
			t.Fatalf("NextOpen(start) errored after Add succeeded: start=%s err=%v", start, err)
		}
		if got := b.Between(anchor, result); got != d {
			t.Fatalf("Add round-trip broke: start=%s d=%s anchor=%s result=%s Between=%s", start, d, anchor, result, got)
		}
	})
}
