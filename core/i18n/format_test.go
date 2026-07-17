package i18n_test

import (
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/i18n"
)

// deSpec is a German-style spec, declared here in the test: core/i18n ships no
// per-locale data, so a formatting test must bring its own.
var deSpec = i18n.FormatSpec{
	DecimalSep:     ",",
	GroupSep:       ".",
	DateLayout:     "02.01.2006",
	TimeLayout:     "15:04",
	DateTimeLayout: "02.01.2006 15:04",
	CurrencySpace:  true,
	PercentSpace:   true,
}

// frSpec uses U+00A0 NO-BREAK SPACE as its group separator, written as an
// escape so the distinction from a plain space stays visible in source.
var frSpec = i18n.FormatSpec{
	DecimalSep:     ",",
	GroupSep:       " ",
	DateLayout:     "02/01/2006",
	TimeLayout:     "15:04",
	DateTimeLayout: "02/01/2006 15:04",
	CurrencySpace:  true,
	PercentSpace:   true,
}

func fmtBundle(t *testing.T) *i18n.Bundle {
	t.Helper()
	b, err := i18n.New(
		i18n.WithMessages(os.DirFS("testdata/locales")),
		i18n.WithFormat("de", deSpec),
		i18n.WithFormat("vi", frSpec), // arbitrary: proves specs are just data
	)
	require.NoError(t, err)
	return b
}

func TestNumberInvariantDefault(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	// uk has no wired spec, so it formats with Invariant — not with anything
	// this package "knows" about Ukrainian, because it knows nothing.
	uk := b.ParseOrDefault("uk")
	assert.Equal(t, "1,234,567.89", b.Number(uk, 1234567.89))
	assert.Equal(t, "0", b.Number(uk, 0))
}

func TestNumberWiredSpec(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	de := b.ParseOrDefault("de")
	assert.Equal(t, "1.234.567,89", b.Number(de, 1234567.89))
	assert.Equal(t, "-1.234,5", b.Number(de, -1234.5))

	vi := b.ParseOrDefault("vi")
	assert.Equal(t, "1 234 567,89", b.Number(vi, 1234567.89))
}

func TestNumberGrouping(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	en := b.Default()
	cases := map[float64]string{
		0:       "0",
		1:       "1",
		12:      "12",
		123:     "123",
		1234:    "1,234",
		12345:   "12,345",
		123456:  "123,456",
		1234567: "1,234,567",
		-1234:   "-1,234",
		-123:    "-123",
		1000:    "1,000",
		1000000: "1,000,000",
	}
	for in, want := range cases {
		assert.Equalf(t, want, b.Number(en, in), "Number(%v)", in)
	}
}

func TestNumberFractions(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	en := b.Default()
	// Shortest round-tripping representation; Number does not round.
	assert.Equal(t, "0.1", b.Number(en, 0.1))
	assert.Equal(t, "1.5", b.Number(en, 1.5))
	assert.Equal(t, "1,234.5678", b.Number(en, 1234.5678))
	assert.Equal(t, "-0.5", b.Number(en, -0.5))
}

func TestNumberInt(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	assert.Equal(t, "1,234,567", b.NumberInt(b.Default(), 1234567))
	assert.Equal(t, "-1.234.567", b.NumberInt(b.ParseOrDefault("de"), -1234567))
	assert.Equal(t, "0", b.NumberInt(b.Default(), 0))
}

// TestNumberIntMinInt64 pins the signed-magnitude conversion: a plain
// negation of math.MinInt64 overflows int64, since its magnitude has no
// int64 representation. The conversion must go through uint64 instead.
func TestNumberIntMinInt64(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	assert.Equal(t, "-9,223,372,036,854,775,808", b.NumberInt(b.Default(), math.MinInt64))
}

func TestPercent(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	assert.Equal(t, "50%", b.Percent(b.Default(), 0.5))
	assert.Equal(t, "12.5%", b.Percent(b.Default(), 0.125))
	assert.Equal(t, "0%", b.Percent(b.Default(), 0))
	assert.Equal(t, "100%", b.Percent(b.Default(), 1))
	// PercentSpace inserts a space before the sign.
	assert.Equal(t, "50 %", b.Percent(b.ParseOrDefault("de"), 0.5))
	assert.Equal(t, "-7,5 %", b.Percent(b.ParseOrDefault("de"), -0.075))
}

func TestNumberZeroLocaleUsesInvariant(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	var zero i18n.Locale
	// The zero Locale resolves to the default (en), which has no wired spec.
	assert.Equal(t, "1,234.5", b.Number(zero, 1234.5))
}

// TestFormatSpecTagBeatsLang covers the first rung of the resolution order:
// an exact-tag spec must be preferred over one wired for the base language.
func TestFormatSpecTagBeatsLang(t *testing.T) {
	t.Parallel()
	gbSpec := i18n.FormatSpec{DecimalSep: ".", GroupSep: " ", DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04"}
	b, err := i18n.New(
		i18n.WithMessages(os.DirFS("testdata/locales")),
		i18n.WithFormat("en-GB", gbSpec),
	)
	require.NoError(t, err)
	// en-GB has its own spec...
	assert.Equal(t, "1 234.5", b.Number(b.ParseOrDefault("en-GB"), 1234.5))
	// ...while en still gets Invariant: an exact-tag spec must not leak to a
	// sibling locale that merely shares the base language.
	assert.Equal(t, "1,234.5", b.Number(b.Default(), 1234.5))
}

// TestFormatSpecTagPriorityOverLang pins the actual priority order, which
// TestFormatSpecTagBeatsLang and TestFormatSpecLangAppliesToRegion each fail
// to: neither of those wires *both* rungs for the same locale, so neither
// forces a choice between them. Here en-GB and its base language en are both
// wired, with specs that disagree — resolution must pick en-GB's own tag
// spec, not fall through to en's language spec.
func TestFormatSpecTagPriorityOverLang(t *testing.T) {
	t.Parallel()
	gbSpec := i18n.FormatSpec{DecimalSep: ".", GroupSep: " ", DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04"}
	enSpec := i18n.FormatSpec{DecimalSep: ",", GroupSep: ".", DateLayout: "2006-01-02", TimeLayout: "15:04", DateTimeLayout: "2006-01-02 15:04"}
	b, err := i18n.New(
		i18n.WithMessages(os.DirFS("testdata/locales")),
		i18n.WithFormat("en-GB", gbSpec),
		i18n.WithFormat("en", enSpec),
	)
	require.NoError(t, err)
	// en-GB's own tag spec wins over en's language spec.
	assert.Equal(t, "1 234.5", b.Number(b.ParseOrDefault("en-GB"), 1234.5))
	// en is its own base language, so it uses its own wired spec.
	assert.Equal(t, "1.234,5", b.Number(b.Default(), 1234.5))
}

// TestFormatSpecLangAppliesToRegion covers the second rung: a spec wired for
// the base language covers a regional variant with no spec of its own.
func TestFormatSpecLangAppliesToRegion(t *testing.T) {
	t.Parallel()
	enSpec := i18n.FormatSpec{DecimalSep: ",", GroupSep: ".", DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04"}
	b, err := i18n.New(
		i18n.WithMessages(os.DirFS("testdata/locales")),
		i18n.WithFormat("en", enSpec),
	)
	require.NoError(t, err)
	assert.Equal(t, "1.234,5", b.Number(b.ParseOrDefault("en-GB"), 1234.5))
}

// TestFormatNeverFallsBackToDefaultLocaleSpec is the load-bearing rule from
// resolveFormat's doc comment, made observable for the first time now that a
// reader (specFor) exists. The bundle's default locale (en) gets an explicit,
// non-Invariant spec; a *different*, supported-but-unconfigured locale (uk)
// must still render with Invariant, not silently inherit en's spec through
// its default-locale status.
//
// TestNumberInvariantDefault alone cannot pin this: in that test the default
// locale (en) is itself unwired, so "uk falls back to Invariant" is equally
// consistent with the (wrong) rule "uk falls back to whatever en uses". Only
// wiring en to something Invariant is not, and then checking uk still gets
// Invariant, tells the two apart.
func TestFormatNeverFallsBackToDefaultLocaleSpec(t *testing.T) {
	t.Parallel()
	enSpec := i18n.FormatSpec{
		DecimalSep: "X", GroupSep: "Y",
		DateLayout: "2006", TimeLayout: "15:04", DateTimeLayout: "2006 15:04",
	}
	b, err := i18n.New(
		i18n.WithMessages(os.DirFS("testdata/locales")),
		i18n.WithFormat("en", enSpec),
	)
	require.NoError(t, err)
	// Sanity check: en itself really did pick up the wired spec.
	require.Equal(t, "1Y234X5", b.Number(b.Default(), 1234.5))

	uk := b.ParseOrDefault("uk")
	require.Equal(t, "uk", uk.Tag(), "uk must be a supported, non-default locale for this test to mean anything")
	// If uk's spec fell back to the default locale's wired spec instead of
	// Invariant, this would render "1Y234X5" too.
	assert.Equal(t, "1,234.5", b.Number(uk, 1234.5))
}

func TestNumberNaNAndInf(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	en := b.Default()
	// Rendering is total: NaN and infinities must render as themselves, not
	// panic or corrupt (the shortest-round-trip path treats every byte of
	// "+Inf"/"NaN" as if it were a digit unless these are special-cased,
	// which for "+Inf" splices a stray group separator in after the sign).
	assert.Equal(t, "NaN", b.Number(en, math.NaN()))
	assert.Equal(t, "+Inf", b.Number(en, math.Inf(1)))
	assert.Equal(t, "-Inf", b.Number(en, math.Inf(-1)))
}

func TestPercentNaNAndInf(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	en := b.Default()
	assert.Equal(t, "NaN%", b.Percent(en, math.NaN()))
	assert.Equal(t, "+Inf%", b.Percent(en, math.Inf(1)))
	assert.Equal(t, "-Inf%", b.Percent(en, math.Inf(-1)))
}

func TestAppendNumberAppends(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	assert.Equal(t, "x:1,234", string(b.AppendNumber([]byte("x:"), b.Default(), 1234)))
	assert.Equal(t, "x:1,234", string(b.AppendNumberInt([]byte("x:"), b.Default(), 1234)))
	assert.Equal(t, "x:50%", string(b.AppendPercent([]byte("x:"), b.Default(), 0.5)))
}

// TestAppendNumberZeroAlloc must NOT run t.Parallel(): testing.AllocsPerRun
// panics if called from a test that has.
func TestAppendNumberZeroAlloc(t *testing.T) {
	b := fmtBundle(t)
	en := b.Default()
	dst := make([]byte, 0, 64)
	allocs := testing.AllocsPerRun(200, func() {
		_ = b.AppendNumber(dst[:0], en, 1234567.89)
	})
	assert.Equal(t, 0.0, allocs, "AppendNumber into a sized buffer must not allocate")
}

// TestAppendNumberIntZeroAlloc mirrors the float case for the integer path.
func TestAppendNumberIntZeroAlloc(t *testing.T) {
	b := fmtBundle(t)
	en := b.Default()
	dst := make([]byte, 0, 32)
	allocs := testing.AllocsPerRun(200, func() {
		_ = b.AppendNumberInt(dst[:0], en, 1234567)
	})
	assert.Equal(t, 0.0, allocs, "AppendNumberInt into a sized buffer must not allocate")
}

// TestAppendPercentZeroAlloc mirrors the float case for the percent path.
func TestAppendPercentZeroAlloc(t *testing.T) {
	b := fmtBundle(t)
	en := b.Default()
	dst := make([]byte, 0, 16)
	allocs := testing.AllocsPerRun(200, func() {
		_ = b.AppendPercent(dst[:0], en, 0.5)
	})
	assert.Equal(t, 0.0, allocs, "AppendPercent into a sized buffer must not allocate")
}
