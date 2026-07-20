package fxrate

import "errors"

var (
	// ErrInvalidSnapshot reports a snapshot that violates construction
	// invariants: empty base or provider, zero as-of time, no rates, a
	// duplicate currency code, or a base self-rate that is not exactly 1.
	ErrInvalidSnapshot = errors.New("fxrate: invalid snapshot")

	// ErrInvalidRate reports a rate value that is zero or negative.
	ErrInvalidRate = errors.New("fxrate: invalid rate")

	// ErrUnknownCurrency reports a currency code the snapshot has no rate for.
	ErrUnknownCurrency = errors.New("fxrate: unknown currency")

	// ErrBaseMismatch reports a source that returned a snapshot denominated in
	// a different base currency than requested.
	ErrBaseMismatch = errors.New("fxrate: base currency mismatch")

	// ErrFetchFailed wraps any failure to obtain a usable snapshot from a
	// RateSource. The underlying cause is joined and matchable via errors.Is.
	ErrFetchFailed = errors.New("fxrate: fetch failed")
)
