package money

import (
	"strconv"
	"strings"

	"github.com/dmitrymomot/forge/core/decimal"
)

// Currency is ISO-4217 currency metadata.
type Currency struct {
	// Code is the ISO-4217 alphabetic code, e.g. "USD".
	Code string
	// Num is the ISO-4217 numeric code, e.g. "840".
	Num string
	// Symbol is a display symbol, e.g. "$". It may equal Code when there is no
	// distinct symbol.
	Symbol string
	// MinorUnits is the number of fractional digits, e.g. USD 2, JPY 0, BHD 3.
	MinorUnits int32
}

// Money couples an exact decimal amount with a Currency. The zero value is a
// zero amount in the zero Currency. Money is immutable; every operation returns
// a new value.
type Money struct {
	amount   decimal.Decimal
	currency Currency
}

// New builds a Money from an exact decimal amount and a currency. The amount is
// stored as-is (full precision); rounding to MinorUnits happens only at Minor,
// Round, String, Allocate, and Split.
func New(amount decimal.Decimal, c Currency) Money {
	return Money{amount: amount, currency: c}
}

// FromMinor builds a Money from an integer count of the currency's minor units,
// e.g. FromMinor(150, USD) is 1.50 USD and FromMinor(500, JPY) is 500 JPY.
func FromMinor(units int64, c Currency) Money {
	return Money{amount: decimal.New(units, c.MinorUnits), currency: c}
}

// Parse parses a decimal string as an amount in currency c. The amount is stored
// at its full parsed precision; it is not rounded to MinorUnits here.
func Parse(s string, c Currency) (Money, error) {
	d, err := decimal.Parse(s)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: d, currency: c}, nil
}

// Amount returns the exact decimal amount.
func (m Money) Amount() decimal.Decimal { return m.amount }

// Currency returns the currency.
func (m Money) Currency() Currency { return m.currency }

// Minor returns the amount rounded to the currency's MinorUnits (banker's
// rounding, decimal.HalfEven) expressed as an integer count of minor units. The
// amount is arbitrary-precision, so for an astronomically large value whose
// minor-unit count does not fit in int64 the result saturates to
// math.MaxInt64/MinInt64; use MinorOK to detect that case.
func (m Money) Minor() int64 {
	n, _ := m.minorChecked()
	return n
}

// MinorOK is Minor with an ok flag: ok is false when the minor-unit count
// overflows int64 (and the returned value has saturated). Allocate and Split use
// this to refuse amounts they cannot split exactly.
func (m Money) MinorOK() (n int64, ok bool) {
	return m.minorChecked()
}

// minorChecked rounds the amount to MinorUnits and returns the integer minor-unit
// count, reporting ok=false when it overflows int64.
func (m Money) minorChecked() (n int64, ok bool) {
	rounded := m.amount.Round(m.currency.MinorUnits, decimal.HalfEven)
	scaled := rounded.Rescale(m.currency.MinorUnits, decimal.HalfEven)
	// scaled has exactly MinorUnits fractional digits; multiplying by
	// 10^MinorUnits yields an integer-valued decimal with scale 0.
	shifted := scaled.Mul(minorFactor(m.currency.MinorUnits))
	s := shifted.Round(0, decimal.HalfEven).String()
	// String has no fractional part now; strip any accidental ".0" defensively.
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		s = s[:dot]
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}

// minorFactor returns 10^n as a Decimal for a small non-negative n.
func minorFactor(n int32) decimal.Decimal {
	f := int64(1)
	for range n {
		f *= 10
	}
	return decimal.FromInt(f)
}

// IsZero reports whether the amount is numerically zero.
func (m Money) IsZero() bool { return m.amount.IsZero() }
