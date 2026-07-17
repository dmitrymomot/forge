package i18n_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/i18n"
	"github.com/dmitrymomot/forge/core/money"
)

var (
	usd = money.Currency{Code: "USD", Num: "840", Symbol: "$", MinorUnits: 2}
	eur = money.Currency{Code: "EUR", Num: "978", Symbol: "€", MinorUnits: 2}
	jpy = money.Currency{Code: "JPY", Num: "392", Symbol: "¥", MinorUnits: 0}
	bhd = money.Currency{Code: "BHD", Num: "048", Symbol: ".د.ب", MinorUnits: 3}
)

// curBundle wires "de" to deSpec (declared in format_test.go): symbol after
// the amount with a space, German separators. "en" gets Invariant: symbol
// before, no space.
func curBundle(t *testing.T) *i18n.Bundle {
	t.Helper()
	b, err := i18n.New(
		i18n.WithMessages(os.DirFS("testdata/locales")),
		i18n.WithFormat("de", deSpec),
	)
	require.NoError(t, err)
	return b
}

func TestCurrencyInvariant(t *testing.T) {
	t.Parallel()
	b := curBundle(t)
	en := b.Default()
	assert.Equal(t, "$1,234.56", b.Currency(en, money.FromMinor(123456, usd)))
	assert.Equal(t, "$0.00", b.Currency(en, money.FromMinor(0, usd)))
	assert.Equal(t, "$0.05", b.Currency(en, money.FromMinor(5, usd)))
	assert.Equal(t, "-$1,234.56", b.Currency(en, money.FromMinor(-123456, usd)))
}

func TestCurrencyWiredSpec(t *testing.T) {
	t.Parallel()
	b := curBundle(t)
	de := b.ParseOrDefault("de")
	// Symbol after the amount, separated by a space; German separators.
	assert.Equal(t, "1.234,56 €", b.Currency(de, money.FromMinor(123456, eur)))
	assert.Equal(t, "-1.234,56 €", b.Currency(de, money.FromMinor(-123456, eur)))
}

func TestCurrencyMultiCurrency(t *testing.T) {
	t.Parallel()
	// The locale contributes separators and placement; money contributes the
	// symbol and the minor units. A German reader viewing USD is correct.
	b := curBundle(t)
	de := b.ParseOrDefault("de")
	assert.Equal(t, "1.234,56 $", b.Currency(de, money.FromMinor(123456, usd)))
}

func TestCurrencyMinorUnits(t *testing.T) {
	t.Parallel()
	b := curBundle(t)
	en := b.Default()
	// JPY has no minor units: no decimal separator at all.
	assert.Equal(t, "¥1,235", b.Currency(en, money.FromMinor(1235, jpy)))
	// BHD has three.
	assert.Equal(t, ".د.ب1.234", b.Currency(en, money.FromMinor(1234, bhd)))
	// Padding: 5 minor units of BHD is 0.005.
	assert.Equal(t, ".د.ب0.005", b.Currency(en, money.FromMinor(5, bhd)))
}

func TestCurrencyZeroLocale(t *testing.T) {
	t.Parallel()
	b := curBundle(t)
	var zero i18n.Locale
	assert.Equal(t, "$1.00", b.Currency(zero, money.FromMinor(100, usd)))
}

// TestCurrencyZeroMoney pins totality against the zero money.Money value: a
// zero amount in the zero (empty) Currency. Rendering must never panic even
// when there is no symbol and no minor-unit information at all.
func TestCurrencyZeroMoney(t *testing.T) {
	t.Parallel()
	b := curBundle(t)
	var m money.Money
	assert.Equal(t, "0", b.Currency(b.Default(), m))
}

func TestAppendCurrencyAppends(t *testing.T) {
	t.Parallel()
	b := curBundle(t)
	got := b.AppendCurrency([]byte("x:"), b.Default(), money.FromMinor(100, usd))
	assert.Equal(t, "x:$1.00", string(got))
}

// TestCurrencyMinInt64Minor pins the signed-magnitude conversion for currency
// rendering the same way TestNumberIntMinInt64 pins it for plain integers: a
// plain negation of math.MinInt64 overflows, so the conversion must route
// through absU64 (shared with the number/percent paths), not a second,
// possibly-buggy copy of the same idiom.
func TestCurrencyMinInt64Minor(t *testing.T) {
	t.Parallel()
	b := curBundle(t)
	en := b.Default()
	// A currency with MinorUnits: 0 makes the minor-unit integer equal to
	// math.MinInt64 itself, so this exercises the magnitude conversion
	// directly rather than via money's own rounding.
	weird := money.Currency{Code: "XXX", Symbol: "X", MinorUnits: 0}
	got := b.Currency(en, money.FromMinor(-9223372036854775808, weird))
	assert.Equal(t, "-X9,223,372,036,854,775,808", got)
}

// TestCurrencyLargeMinorUnitsNeverPanics pins totality against a currency
// with a pathologically large MinorUnits value: naively computing 10^units
// in a uint64 would silently wrap (and can wrap to exactly zero), which turns
// the following division into a divide-by-zero panic. Rendering must stay
// total no matter what a Currency value claims.
func TestCurrencyLargeMinorUnitsNeverPanics(t *testing.T) {
	t.Parallel()
	b := curBundle(t)
	en := b.Default()
	weird := money.Currency{Code: "YYY", Symbol: "Y", MinorUnits: 64}
	assert.NotPanics(t, func() {
		_ = b.Currency(en, money.FromMinor(123, weird))
	})
}

func TestAppendCurrencyAddsNoAllocsOverMoney(t *testing.T) {
	// Must NOT run t.Parallel(): testing.AllocsPerRun panics in a parallel test.
	b := curBundle(t)
	en := b.Default()
	m := money.FromMinor(123456, usd)

	// money.MinorOK allocates internally (decimal arithmetic) and that floor is
	// unreachable from this package. Measure it, then assert this package adds
	// nothing on top — an absolute 0 assertion would be unfalsifiable, since
	// AppendCurrency can never go below money's own allocation floor.
	base := testing.AllocsPerRun(200, func() {
		_, _ = m.MinorOK()
	})
	dst := make([]byte, 0, 64)
	got := testing.AllocsPerRun(200, func() {
		_ = b.AppendCurrency(dst[:0], en, m)
	})
	assert.LessOrEqualf(t, got, base, "AppendCurrency must add no allocations over money's floor (base=%v)", base)
}
