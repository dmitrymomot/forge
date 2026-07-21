package tariff

import "errors"

// ErrInvalidMode is returned by New (and Schedule.UnmarshalJSON) when the mode
// is neither Graduated nor Volume.
var ErrInvalidMode = errors.New("tariff: invalid mode")

// ErrNoBands is returned by New when no bands are given, and by Apply,
// ApplyMoney, and MarshalJSON on a zero Schedule.
var ErrNoBands = errors.New("tariff: schedule has no bands")

// ErrOpenBand is returned by New when the final band is not open, when a
// non-final band is open, or when an open band carries an upper bound.
var ErrOpenBand = errors.New("tariff: exactly the final band must be open")

// ErrBandOrder is returned by New when a bounded band's upper bound is not
// positive or does not strictly exceed the previous band's bound.
var ErrBandOrder = errors.New("tariff: band bounds must be positive and strictly increasing")

// ErrNegativeRate is returned by New when any band's rate is negative.
var ErrNegativeRate = errors.New("tariff: negative rate")

// ErrNegativeBase is returned by Apply and ApplyMoney when the base is
// negative.
var ErrNegativeBase = errors.New("tariff: negative base")
