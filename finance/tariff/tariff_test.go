package tariff_test

import (
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/tariff"
)

func d(s string) decimal.Decimal { return decimal.MustParse(s) }

// catalogSchedule is the catalog example: 25% up to 10, 30% to 50, 35% above.
func catalogSchedule(t *testing.T, mode tariff.Mode) tariff.Schedule {
	t.Helper()
	s, err := tariff.New(mode,
		tariff.UpTo(d("10"), d("0.25")),
		tariff.UpTo(d("50"), d("0.30")),
		tariff.Above(d("0.35")),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		mode  tariff.Mode
		bands []tariff.Band
		want  error
	}{
		{"invalid mode", tariff.Mode("banded"), []tariff.Band{tariff.Above(d("0.1"))}, tariff.ErrInvalidMode},
		{"empty mode", tariff.Mode(""), []tariff.Band{tariff.Above(d("0.1"))}, tariff.ErrInvalidMode},
		{"no bands", tariff.Graduated, nil, tariff.ErrNoBands},
		{"last band not open", tariff.Graduated, []tariff.Band{tariff.UpTo(d("10"), d("0.25"))}, tariff.ErrOpenBand},
		{"open band not last", tariff.Graduated, []tariff.Band{tariff.Above(d("0.25")), tariff.Above(d("0.30"))}, tariff.ErrOpenBand},
		{"open band with bound", tariff.Graduated, []tariff.Band{{UpTo: d("10"), Rate: d("0.25"), Open: true}}, tariff.ErrOpenBand},
		{"zero bound", tariff.Graduated, []tariff.Band{tariff.UpTo(d("0"), d("0.25")), tariff.Above(d("0.30"))}, tariff.ErrBandOrder},
		{"negative bound", tariff.Graduated, []tariff.Band{tariff.UpTo(d("-1"), d("0.25")), tariff.Above(d("0.30"))}, tariff.ErrBandOrder},
		{"non-increasing bounds", tariff.Graduated, []tariff.Band{tariff.UpTo(d("10"), d("0.25")), tariff.UpTo(d("10"), d("0.30")), tariff.Above(d("0.35"))}, tariff.ErrBandOrder},
		{"decreasing bounds", tariff.Volume, []tariff.Band{tariff.UpTo(d("50"), d("0.25")), tariff.UpTo(d("10"), d("0.30")), tariff.Above(d("0.35"))}, tariff.ErrBandOrder},
		{"negative rate", tariff.Graduated, []tariff.Band{tariff.UpTo(d("10"), d("-0.25")), tariff.Above(d("0.30"))}, tariff.ErrNegativeRate},
		{"negative open rate", tariff.Volume, []tariff.Band{tariff.Above(d("-0.1"))}, tariff.ErrNegativeRate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tariff.New(tc.mode, tc.bands...)
			if !errors.Is(err, tc.want) {
				t.Fatalf("New = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewValid(t *testing.T) {
	t.Parallel()

	// Single open band: a flat rate.
	flat, err := tariff.New(tariff.Volume, tariff.Above(d("0.15")))
	if err != nil {
		t.Fatalf("New flat: %v", err)
	}
	if flat.Mode() != tariff.Volume {
		t.Fatalf("Mode = %q, want %q", flat.Mode(), tariff.Volume)
	}
	if n := len(flat.Bands()); n != 1 {
		t.Fatalf("len(Bands) = %d, want 1", n)
	}

	// Zero rate is a valid free tier; scale-differing bounds still order
	// correctly (10.00 < 10.5).
	if _, err := tariff.New(tariff.Graduated,
		tariff.UpTo(d("10.00"), d("0")),
		tariff.UpTo(d("10.5"), d("0.30")),
		tariff.Above(d("0.35")),
	); err != nil {
		t.Fatalf("New free tier: %v", err)
	}
}

func TestScheduleImmutable(t *testing.T) {
	t.Parallel()

	bands := []tariff.Band{
		tariff.UpTo(d("10"), d("0.25")),
		tariff.Above(d("0.35")),
	}
	s, err := tariff.New(tariff.Graduated, bands...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// The schedule must not see mutations of the caller's slice or of a
	// Bands() copy: the bounded band's rate stays 0.25 throughout.
	check := func(context string) {
		t.Helper()
		for _, b := range s.Bands() {
			if !b.Open && !b.Rate.Equal(d("0.25")) {
				t.Fatalf("schedule saw %s: rate = %s", context, b.Rate)
			}
		}
	}

	bands[0].Rate = d("0.99")
	check("caller mutation")

	clone := s.Bands()
	for i := range clone {
		clone[i].Rate = d("0.77")
	}
	check("accessor mutation")
}

func TestApplyGraduated(t *testing.T) {
	t.Parallel()
	s := catalogSchedule(t, tariff.Graduated)

	cases := []struct {
		base   string
		total  string
		slices []string
	}{
		{"7", "1.75", []string{"7"}},                             // inside first band
		{"10", "2.50", []string{"10"}},                           // exactly on first bound
		{"10.5", "2.65", []string{"10", "0.5"}},                  // just over first bound
		{"50", "14.50", []string{"10", "40"}},                    // exactly on second bound
		{"100", "32.00", []string{"10", "40", "50"}},             // catalog example
		{"1000000", "349997.00", []string{"10", "40", "999950"}}, // deep in open band
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			t.Parallel()
			res, err := s.Apply(d(tc.base))
			if err != nil {
				t.Fatalf("Apply(%s): %v", tc.base, err)
			}
			if res.Total.Cmp(d(tc.total)) != 0 {
				t.Fatalf("Total = %s, want %s", res.Total, tc.total)
			}
			if len(res.Lines) != len(tc.slices) {
				t.Fatalf("len(Lines) = %d, want %d", len(res.Lines), len(tc.slices))
			}
			sum := decimal.Zero
			for i, l := range res.Lines {
				if l.Band != i {
					t.Fatalf("line %d Band = %d", i, l.Band)
				}
				if l.Slice.Cmp(d(tc.slices[i])) != 0 {
					t.Fatalf("line %d Slice = %s, want %s", i, l.Slice, tc.slices[i])
				}
				if !l.Amount.Equal(l.Slice.Mul(l.Rate)) {
					t.Fatalf("line %d Amount = %s, want Slice×Rate = %s", i, l.Amount, l.Slice.Mul(l.Rate))
				}
				sum = sum.Add(l.Slice)
			}
			if sum.Cmp(d(tc.base)) != 0 {
				t.Fatalf("slices sum to %s, want base %s", sum, tc.base)
			}
		})
	}
}

func TestApplyVolume(t *testing.T) {
	t.Parallel()
	s := catalogSchedule(t, tariff.Volume)

	cases := []struct {
		base  string
		band  int
		total string
	}{
		{"7", 0, "1.75"},
		{"10", 0, "2.50"},     // bounds are inclusive
		{"10.01", 1, "3.003"}, // just past the bound → next band
		{"50", 1, "15.00"},
		{"50.5", 2, "17.675"},
		{"100", 2, "35.00"},
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			t.Parallel()
			res, err := s.Apply(d(tc.base))
			if err != nil {
				t.Fatalf("Apply(%s): %v", tc.base, err)
			}
			if len(res.Lines) != 1 {
				t.Fatalf("len(Lines) = %d, want 1", len(res.Lines))
			}
			l := res.Lines[0]
			if l.Band != tc.band {
				t.Fatalf("Band = %d, want %d", l.Band, tc.band)
			}
			if l.Slice.Cmp(d(tc.base)) != 0 {
				t.Fatalf("Slice = %s, want whole base %s", l.Slice, tc.base)
			}
			if res.Total.Cmp(d(tc.total)) != 0 {
				t.Fatalf("Total = %s, want %s", res.Total, tc.total)
			}
		})
	}
}

func TestApplyZeroBase(t *testing.T) {
	t.Parallel()
	for _, mode := range []tariff.Mode{tariff.Graduated, tariff.Volume} {
		s := catalogSchedule(t, mode)
		res, err := s.Apply(decimal.Zero)
		if err != nil {
			t.Fatalf("%s: Apply(0): %v", mode, err)
		}
		if len(res.Lines) != 0 {
			t.Fatalf("%s: len(Lines) = %d, want 0", mode, len(res.Lines))
		}
		if !res.Total.IsZero() {
			t.Fatalf("%s: Total = %s, want 0", mode, res.Total)
		}
	}
}

func TestApplyNegativeBase(t *testing.T) {
	t.Parallel()
	s := catalogSchedule(t, tariff.Graduated)
	if _, err := s.Apply(d("-1")); !errors.Is(err, tariff.ErrNegativeBase) {
		t.Fatalf("Apply(-1) = %v, want ErrNegativeBase", err)
	}
}

func TestApplyZeroSchedule(t *testing.T) {
	t.Parallel()
	var s tariff.Schedule
	if _, err := s.Apply(d("10")); !errors.Is(err, tariff.ErrNoBands) {
		t.Fatalf("zero Schedule Apply = %v, want ErrNoBands", err)
	}
}

func TestApplyFreeTier(t *testing.T) {
	t.Parallel()
	s, err := tariff.New(tariff.Graduated,
		tariff.UpTo(d("1000"), d("0")),
		tariff.Above(d("0.01")),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := s.Apply(d("1500"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// The free tier still emits its statement line with a zero amount.
	if len(res.Lines) != 2 {
		t.Fatalf("len(Lines) = %d, want 2", len(res.Lines))
	}
	if !res.Lines[0].Amount.IsZero() {
		t.Fatalf("free-tier Amount = %s, want 0", res.Lines[0].Amount)
	}
	if res.Total.Cmp(d("5.00")) != 0 {
		t.Fatalf("Total = %s, want 5.00", res.Total)
	}
}

func TestApplyExactness(t *testing.T) {
	t.Parallel()
	// High-scale rate and base: products must be exact, never float-drifted.
	s, err := tariff.New(tariff.Volume, tariff.Above(d("0.0333")))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := s.Apply(d("12345.6789"))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := res.Total.String(), "411.11110737"; got != want {
		t.Fatalf("Total = %s, want %s", got, want)
	}
}

func TestApplyGraduatedMonotone(t *testing.T) {
	t.Parallel()
	s := catalogSchedule(t, tariff.Graduated)
	bases := []string{"0", "1", "9.99", "10", "10.01", "25", "50", "50.01", "75", "100", "12345"}
	prev := decimal.Zero.Sub(d("1"))
	prevTotal := decimal.Zero
	for _, b := range bases {
		res, err := s.Apply(d(b))
		if err != nil {
			t.Fatalf("Apply(%s): %v", b, err)
		}
		if res.Total.Cmp(prevTotal) < 0 {
			t.Fatalf("total decreased: Apply(%s) = %s after %s at base %s", b, res.Total, prevTotal, prev)
		}
		prev, prevTotal = d(b), res.Total
	}
}
