package i18n_test

import (
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/i18n"
	"github.com/dmitrymomot/forge/core/money"
)

func TestLocalizer(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	l := b.For(b.ParseOrDefault("uk"))

	assert.Equal(t, "uk", l.Locale().Tag())
	assert.False(t, l.IsZero())
	assert.Equal(t, "Панель", l.T("app.title"))
	assert.Equal(t, "Привіт, Олю!", l.T("app.greeting", "name", "Олю"))
	// Falls through to the default, exactly like Bundle.T.
	assert.Equal(t, "English only", l.T("app.only_en"))
}

// TestLocalizerMatchesBundle is the falsifiable delegation check: en, de, uk
// and vi each render numbers/dates differently (Invariant vs. deSpec vs.
// frSpec), so a Localizer that cached the wrong locale index, or that
// re-implemented formatting instead of delegating, would diverge from the
// equivalent Bundle call for at least one of them.
func TestLocalizerMatchesBundle(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	m := money.FromMinor(123456, money.Currency{Code: "EUR", Num: "978", Symbol: "€", MinorUnits: 2})

	for _, tag := range []string{"en", "de", "uk", "vi"} {
		loc := b.ParseOrDefault(tag)
		l := b.For(loc)
		assert.Equalf(t, b.T(loc, "app.title"), l.T("app.title"), "T mismatch for %s", tag)
		assert.Equalf(t, b.TN(loc, "cart.items", 3), l.TN("cart.items", 3), "TN mismatch for %s", tag)
		assert.Equalf(t, b.Number(loc, 1234.5), l.Number(1234.5), "Number mismatch for %s", tag)
		assert.Equalf(t, b.NumberInt(loc, 1234), l.NumberInt(1234), "NumberInt mismatch for %s", tag)
		assert.Equalf(t, b.Percent(loc, 0.5), l.Percent(0.5), "Percent mismatch for %s", tag)
		assert.Equalf(t, b.Currency(loc, m), l.Currency(m), "Currency mismatch for %s", tag)
		assert.Equalf(t, b.Date(loc, ts), l.Date(ts), "Date mismatch for %s", tag)
		assert.Equalf(t, b.Time(loc, ts), l.Time(ts), "Time mismatch for %s", tag)
		assert.Equalf(t, b.DateTime(loc, ts), l.DateTime(ts), "DateTime mismatch for %s", tag)
	}
}

func TestLocalizerKeys(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	l := b.For(b.Default())
	assert.Equal(t, "Dashboard", l.TK(keyTitle))
	assert.Equal(t, "1 item in your cart", l.TNK(keyItems, 1))
}

func TestZeroLocalizerIsSafe(t *testing.T) {
	t.Parallel()
	// The zero Localizer is what a request that never hit the middleware
	// yields. It must fail closed, never panic.
	var l i18n.Localizer
	assert.True(t, l.IsZero())
	assert.True(t, l.Locale().IsZero())

	assert.Equal(t, "app.title", l.T("app.title"))
	assert.Equal(t, "cart.items", l.TN("cart.items", 5))
	assert.Equal(t, "app.title", l.TK(keyTitle))
	assert.Equal(t, "cart.items", l.TNK(keyItems, 5))
	// Formatting falls back to Invariant.
	assert.Equal(t, "1,234.5", l.Number(1234.5))
	assert.Equal(t, "1,234", l.NumberInt(1234))
	assert.Equal(t, "50%", l.Percent(0.5))
	assert.Equal(t, "$1.00", l.Currency(money.FromMinor(100, money.Currency{Code: "USD", Symbol: "$", MinorUnits: 2})))
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	assert.Equal(t, "2026-07-17", l.Date(ts))
	assert.Equal(t, "15:04", l.Time(ts))
	assert.Equal(t, "2026-07-17 15:04", l.DateTime(ts))
}

// TestLocalizerIsTwoWords asserts the real, falsifiable size of Localizer:
// exactly two machine words (a *Bundle pointer plus the cached locale index),
// on any GOARCH. This would fail if Localizer grew a third field (e.g. an
// inlined Locale instead of the cached int index) or otherwise stopped being
// cheap to copy into ctx and template data.
//
// A zero-alloc check on For alone cannot verify this: discarding a returned
// value type into `_` inside a closure never allocates or escapes for any
// struct shape, so it is non-falsifiable for the "two words" size property.
// See TestForDoesNotAllocate below for that, kept as a separate concern.
func TestLocalizerIsTwoWords(t *testing.T) {
	t.Parallel()
	wantSize := 2 * unsafe.Sizeof(uintptr(0))
	assert.Equal(t, wantSize, reflect.TypeFor[i18n.Localizer]().Size(), "Localizer must stay exactly two machine words")
}

// TestForDoesNotAllocate is a separate, narrower claim than the size test
// above: building a Localizer does no heap work (locIdx is a map lookup, not
// string construction). Must not run t.Parallel(): testing.AllocsPerRun
// panics inside a parallel test.
func TestForDoesNotAllocate(t *testing.T) {
	b := fmtBundle(t)
	def := b.Default()
	allocs := testing.AllocsPerRun(200, func() {
		_ = b.For(def)
	})
	assert.Equal(t, 0.0, allocs, "For must not allocate")
}
