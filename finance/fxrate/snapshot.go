package fxrate

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/decimal"
)

// Snapshot is an immutable table of exchange rates against one base currency
// as of a point in time. It is the unit of storage: persist it (JSON) next to
// the transaction and any later recompute reproduces the same numbers.
// The zero value is empty and unusable; construct via NewSnapshot.
type Snapshot struct {
	asOf     time.Time
	rates    map[string]decimal.Decimal
	base     string
	provider string
}

// NewSnapshot builds a validated snapshot. Currency codes are trimmed and
// uppercased; rates must be strictly positive; an entry for base itself is
// permitted only when exactly 1 and is not stored. Construction fails closed:
// empty base or provider, zero asOf, no rates, duplicate codes (after
// normalization), and non-positive rates are all rejected. The rates map is
// copied, so later caller mutation cannot corrupt the snapshot.
func NewSnapshot(base, provider string, asOf time.Time, rates map[string]decimal.Decimal) (Snapshot, error) {
	base = normalizeCode(base)
	if base == "" {
		return Snapshot{}, fmt.Errorf("%w: empty base currency", ErrInvalidSnapshot)
	}
	if provider == "" {
		return Snapshot{}, fmt.Errorf("%w: empty provider", ErrInvalidSnapshot)
	}
	if asOf.IsZero() {
		return Snapshot{}, fmt.Errorf("%w: zero as-of time", ErrInvalidSnapshot)
	}

	normalized := make(map[string]decimal.Decimal, len(rates))
	for code, value := range rates {
		code = normalizeCode(code)
		if code == "" {
			return Snapshot{}, fmt.Errorf("%w: empty currency code", ErrInvalidSnapshot)
		}
		if value.Sign() <= 0 {
			return Snapshot{}, fmt.Errorf("%w: %s = %s", ErrInvalidRate, code, value)
		}
		if code == base {
			if !value.Equal(one) {
				return Snapshot{}, fmt.Errorf("%w: base self-rate %s = %s, want 1", ErrInvalidRate, base, value)
			}
			continue
		}
		if _, dup := normalized[code]; dup {
			return Snapshot{}, fmt.Errorf("%w: duplicate currency %s", ErrInvalidSnapshot, code)
		}
		normalized[code] = value
	}
	if len(normalized) == 0 {
		return Snapshot{}, fmt.Errorf("%w: no rates", ErrInvalidSnapshot)
	}

	return Snapshot{asOf: asOf, base: base, provider: provider, rates: normalized}, nil
}

// Base returns the currency all stored rates are denominated against.
func (s Snapshot) Base() string { return s.base }

// Provider returns the rate source name recorded at construction.
func (s Snapshot) Provider() string { return s.provider }

// AsOf returns the provider's publication time for these rates.
func (s Snapshot) AsOf() time.Time { return s.asOf }

// IsZero reports whether s is the unusable zero value.
func (s Snapshot) IsZero() bool { return s.rates == nil }

// Has reports whether the snapshot can price code — the base itself or any
// quoted currency.
func (s Snapshot) Has(code string) bool {
	if s.rates == nil {
		return false
	}
	code = normalizeCode(code)
	if code == s.base {
		return true
	}
	_, ok := s.rates[code]
	return ok
}

// Currencies returns every currency the snapshot can price — the base plus
// all quotes — sorted for deterministic iteration.
func (s Snapshot) Currencies() []string {
	if s.rates == nil {
		return nil
	}
	codes := make([]string, 0, len(s.rates)+1)
	codes = append(codes, s.base)
	for code := range s.rates {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	return codes
}

// Rate returns the rate converting from into to. Direct quotes (from == base)
// keep the provider's scale; inverse and cross rates are derived by division
// at RateScale digits, half-to-even, so recomputes are deterministic.
// Unknown currencies fail with ErrUnknownCurrency.
func (s Snapshot) Rate(from, to string) (Rate, error) {
	from = normalizeCode(from)
	to = normalizeCode(to)
	if !s.Has(from) {
		return Rate{}, fmt.Errorf("%w: %s", ErrUnknownCurrency, from)
	}
	if !s.Has(to) {
		return Rate{}, fmt.Errorf("%w: %s", ErrUnknownCurrency, to)
	}

	r := Rate{AsOf: s.asOf, Base: from, Quote: to, Provider: s.provider}
	switch {
	case from == to:
		r.Value = one
	case from == s.base:
		r.Value = s.rates[to]
	case to == s.base:
		v, err := one.Div(s.rates[from], RateScale, decimal.HalfEven)
		if err != nil {
			return Rate{}, fmt.Errorf("%w: invert %s: %w", ErrInvalidRate, from, err)
		}
		r.Value = v
	default:
		v, err := s.rates[to].Div(s.rates[from], RateScale, decimal.HalfEven)
		if err != nil {
			return Rate{}, fmt.Errorf("%w: cross %s/%s: %w", ErrInvalidRate, to, from, err)
		}
		r.Value = v
	}
	return r, nil
}

// Convert converts amount from one currency to another: rate lookup, exact
// multiply, one rounding to scale digits with mode. The returned Conversion
// records the applied rate for audit.
func (s Snapshot) Convert(amount decimal.Decimal, from, to string, scale int32, mode decimal.RoundingMode) (Conversion, error) {
	r, err := s.Rate(from, to)
	if err != nil {
		return Conversion{}, err
	}
	return r.Apply(amount, scale, mode), nil
}

// snapshotJSON is the wire form of Snapshot. Rates marshal as strings (the
// decimal package's exact form), never floats.
type snapshotJSON struct {
	AsOf     time.Time                  `json:"as_of"`
	Rates    map[string]decimal.Decimal `json:"rates"`
	Base     string                     `json:"base"`
	Provider string                     `json:"provider"`
}

// MarshalJSON implements json.Marshaler.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(snapshotJSON{AsOf: s.asOf, Base: s.base, Provider: s.provider, Rates: s.rates})
}

// UnmarshalJSON implements json.Unmarshaler. The decoded snapshot passes
// through NewSnapshot, so corrupt or tampered stored data fails closed
// instead of producing an invalid table.
func (s *Snapshot) UnmarshalJSON(p []byte) error {
	var v snapshotJSON
	if err := json.Unmarshal(p, &v); err != nil {
		return err
	}
	snap, err := NewSnapshot(v.Base, v.Provider, v.AsOf, v.Rates)
	if err != nil {
		return err
	}
	*s = snap
	return nil
}
