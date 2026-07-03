package money

import (
	"github.com/dmitrymomot/forge/core/decimal"
)

// Add returns m + n. It requires m and n to share a currency, otherwise it
// returns ErrCurrencyMismatch. The result keeps full precision (no rounding).
func (m Money) Add(n Money) (Money, error) {
	if m.currency.Code != n.currency.Code {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{amount: m.amount.Add(n.amount), currency: m.currency}, nil
}

// Sub returns m - n. It requires a matching currency, otherwise
// ErrCurrencyMismatch. The result keeps full precision.
func (m Money) Sub(n Money) (Money, error) {
	if m.currency.Code != n.currency.Code {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{amount: m.amount.Sub(n.amount), currency: m.currency}, nil
}

// Mul multiplies the amount by an exact decimal factor (e.g. a tax rate),
// preserving full precision. The caller Rounds the result for settlement.
func (m Money) Mul(factor decimal.Decimal) Money {
	return Money{amount: m.amount.Mul(factor), currency: m.currency}
}

// Round returns m with its amount rounded to the currency's MinorUnits using
// the given rounding mode.
func (m Money) Round(mode decimal.RoundingMode) Money {
	return Money{amount: m.amount.Round(m.currency.MinorUnits, mode), currency: m.currency}
}

// Cmp compares m and n numerically: -1 if m < n, 0 if equal, +1 if m > n. It
// requires a matching currency, otherwise ErrCurrencyMismatch.
func (m Money) Cmp(n Money) (int, error) {
	if m.currency.Code != n.currency.Code {
		return 0, ErrCurrencyMismatch
	}
	return m.amount.Cmp(n.amount), nil
}

// String renders the amount at MinorUnits precision followed by the currency
// code, e.g. "1.50 USD", "500 JPY", "1.234 BHD". It is locale-free and
// unambiguous; localized formatting is deferred to a future i18n layer.
func (m Money) String() string {
	rounded := m.amount.Round(m.currency.MinorUnits, decimal.HalfEven)
	return rounded.Rescale(m.currency.MinorUnits, decimal.HalfEven).String() + " " + m.currency.Code
}
