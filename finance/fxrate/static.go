package fxrate

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/core/decimal"
)

// StaticSource serves a fixed Snapshot: the RateSource for tests and for
// deployments with contractually fixed rates (a per-deal rate card is one
// StaticSource per deal). It never performs I/O.
type StaticSource struct {
	snap Snapshot
}

// NewStaticSource wraps snap as a RateSource.
func NewStaticSource(snap Snapshot) (*StaticSource, error) {
	if snap.IsZero() {
		return nil, fmt.Errorf("%w: zero snapshot", ErrInvalidSnapshot)
	}
	return &StaticSource{snap: snap}, nil
}

// Fetch implements RateSource. The requested base must match the wrapped
// snapshot's base; with quotes given, the result is narrowed to exactly those
// currencies and fails with ErrUnknownCurrency if any is missing.
func (s *StaticSource) Fetch(_ context.Context, base string, quotes []string) (Snapshot, error) {
	if normalizeCode(base) != s.snap.Base() {
		return Snapshot{}, fmt.Errorf("%w: have %s, requested %s", ErrBaseMismatch, s.snap.Base(), base)
	}
	if len(quotes) == 0 {
		return s.snap, nil
	}
	rates := make(map[string]decimal.Decimal, len(quotes))
	for _, q := range quotes {
		q = normalizeCode(q)
		if q == s.snap.base {
			continue
		}
		v, ok := s.snap.rates[q]
		if !ok {
			return Snapshot{}, fmt.Errorf("%w: %s", ErrUnknownCurrency, q)
		}
		rates[q] = v
	}
	return NewSnapshot(s.snap.base, s.snap.provider, s.snap.asOf, rates)
}
