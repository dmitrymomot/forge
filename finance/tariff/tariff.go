package tariff

import (
	"fmt"
	"slices"

	"github.com/dmitrymomot/forge/core/decimal"
)

// Mode selects how a schedule's bands rate a base.
type Mode string

const (
	// Graduated rates each slice of the base by its own band, like tax
	// brackets: a base of 100 against "25% up to 10, 30% to 50, 35% above"
	// pays 25% on the first 10, 30% on the next 40, and 35% on the final 50.
	Graduated Mode = "graduated"
	// Volume rates the entire base by the single band the base falls into:
	// a base of 100 against the same schedule pays 35% on all 100.
	Volume Mode = "volume"
)

// Band is one rate step of a Schedule. A schedule's bands partition the
// non-negative base axis: band i covers (previous bound, UpTo], and the final
// band is Open — it has no upper bound and covers everything above the last
// bound. Construct bands with UpTo and Above rather than struct literals.
type Band struct {
	// UpTo is the band's inclusive upper bound. It must be zero on an open
	// band (New rejects an open band that carries a bound).
	UpTo decimal.Decimal
	// Rate is the multiplier applied to base amounts in this band: a fraction
	// for percentage schedules (0.25 = 25%) or a unit price for usage
	// pricing. Zero is a valid rate (a free tier); negative rates are
	// rejected.
	Rate decimal.Decimal
	// Open marks the final, unbounded band.
	Open bool
}

// UpTo returns a bounded band: rate applies up to and including bound.
func UpTo(bound, rate decimal.Decimal) Band {
	return Band{UpTo: bound, Rate: rate}
}

// Above returns the open final band: rate applies to everything above the
// previous band's bound.
func Above(rate decimal.Decimal) Band {
	return Band{Rate: rate, Open: true}
}

// Schedule is a validated, immutable band set with a rating mode. The zero
// value is invalid; construct with New and share freely — Schedule is safe
// for concurrent use.
type Schedule struct {
	mode  Mode
	bands []Band
}

// New validates the band set and returns a Schedule. The final band must be
// created with Above (open), every earlier band with UpTo; bounds must be
// positive and strictly increasing, rates non-negative.
func New(mode Mode, bands ...Band) (Schedule, error) {
	if mode != Graduated && mode != Volume {
		return Schedule{}, fmt.Errorf("%w: %q", ErrInvalidMode, string(mode))
	}
	if len(bands) == 0 {
		return Schedule{}, ErrNoBands
	}
	for i, b := range bands {
		last := i == len(bands)-1
		if b.Open != last {
			return Schedule{}, fmt.Errorf("%w: band %d", ErrOpenBand, i)
		}
		if b.Open && !b.UpTo.IsZero() {
			return Schedule{}, fmt.Errorf("%w: open band %d carries an upper bound", ErrOpenBand, i)
		}
		if b.Rate.IsNegative() {
			return Schedule{}, fmt.Errorf("%w: band %d", ErrNegativeRate, i)
		}
		if !b.Open {
			if !b.UpTo.IsPositive() {
				return Schedule{}, fmt.Errorf("%w: band %d", ErrBandOrder, i)
			}
			if i > 0 && b.UpTo.Cmp(bands[i-1].UpTo) <= 0 {
				return Schedule{}, fmt.Errorf("%w: band %d", ErrBandOrder, i)
			}
		}
	}
	return Schedule{bands: slices.Clone(bands), mode: mode}, nil
}

// Mode reports the schedule's rating mode.
func (s Schedule) Mode() Mode { return s.mode }

// Bands returns a copy of the schedule's bands.
func (s Schedule) Bands() []Band { return slices.Clone(s.bands) }

// Line is one band's contribution to a Result.
type Line struct {
	// Slice is the portion of the base rated by this band: the in-band slice
	// under Graduated, the entire base under Volume.
	Slice decimal.Decimal
	// Rate is the band's rate, echoed for statement rendering.
	Rate decimal.Decimal
	// Amount is Slice × Rate, exact (never rounded).
	Amount decimal.Decimal
	// Band is the index of the band in the schedule.
	Band int
}

// Result is the exact outcome of rating a base against a schedule.
type Result struct {
	// Lines holds one entry per band that rated a non-empty slice, in band
	// order. A zero base produces no lines.
	Lines []Line
	// Total is the exact sum of the line amounts.
	Total decimal.Decimal
}

// Apply rates a non-negative base against the schedule and returns the exact
// per-band breakdown. Under Graduated each band rates its own slice of the
// base; under Volume the single band containing the base (bounds inclusive)
// rates the whole base. Amounts are exact decimal products — rounding is the
// caller's settlement step (decimal.Round, or money.Round via ApplyMoney).
func (s Schedule) Apply(base decimal.Decimal) (Result, error) {
	if len(s.bands) == 0 {
		return Result{}, ErrNoBands
	}
	if base.IsNegative() {
		return Result{}, ErrNegativeBase
	}
	if base.IsZero() {
		return Result{}, nil
	}
	if s.mode == Volume {
		idx := len(s.bands) - 1
		for i, b := range s.bands[:idx] {
			if base.Cmp(b.UpTo) <= 0 {
				idx = i
				break
			}
		}
		amount := base.Mul(s.bands[idx].Rate)
		return Result{
			Lines: []Line{{Band: idx, Slice: base, Rate: s.bands[idx].Rate, Amount: amount}},
			Total: amount,
		}, nil
	}

	lines := make([]Line, 0, len(s.bands))
	total := decimal.Zero
	prev := decimal.Zero
	for i, b := range s.bands {
		hi := base
		if !b.Open && b.UpTo.Cmp(base) < 0 {
			hi = b.UpTo
		}
		slice := hi.Sub(prev)
		amount := slice.Mul(b.Rate)
		lines = append(lines, Line{Band: i, Slice: slice, Rate: b.Rate, Amount: amount})
		total = total.Add(amount)
		if hi.Cmp(base) == 0 {
			break
		}
		prev = b.UpTo
	}
	return Result{Lines: lines, Total: total}, nil
}
