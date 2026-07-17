package cldr_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/i18n"
	"github.com/dmitrymomot/forge/core/i18n/cldr"
	"github.com/dmitrymomot/forge/core/money"
)

// wantLocales is the full set of locale tags this package must carry. It
// pins the count so a var that is written but never added to allFormats (or
// vice versa) is caught.
var wantLocales = []string{
	"en-US", "en-GB",
	"de-DE", "fr-FR",
	"es-ES", "es-MX", "es-AR", "es-CO", "es-CL",
	"it-IT",
	"pt-BR", "pt-PT",
	"nl-NL",
	"pl-PL", "cs-CZ", "uk-UA", "ru-RU",
	"tr-TR",
	"ar-AE",
	"ja-JP", "zh-CN", "ko-KR",
}

func TestFormatSpecsAreComplete(t *testing.T) {
	t.Parallel()
	all := cldr.AllFormats()
	assert.Len(t, all, len(wantLocales), "allFormats size drifted from the locale list this package promises")
	for _, tag := range wantLocales {
		s, ok := all[tag]
		require.Truef(t, ok, "%s: missing from AllFormats", tag)
		assert.NotEmptyf(t, s.DecimalSep, "%s: empty DecimalSep", tag)
		assert.NotEmptyf(t, s.GroupSep, "%s: empty GroupSep", tag)
		assert.NotEmptyf(t, s.DateLayout, "%s: empty DateLayout", tag)
		assert.NotEmptyf(t, s.TimeLayout, "%s: empty TimeLayout", tag)
		assert.NotEmptyf(t, s.DateTimeLayout, "%s: empty DateTimeLayout", tag)
		assert.NotEqualf(t, s.DecimalSep, s.GroupSep, "%s: separators must differ", tag)
		// core/i18n's grouping engine writes into a fixed [64]byte buffer and
		// inserts GroupSep between every 3-digit group; a pathologically long
		// separator could drive the write position negative. Every real CLDR
		// separator is 1-2 bytes; this is cheap, alongside the render tests
		// below, not instead of them.
		assert.LessOrEqualf(t, len(s.GroupSep), 4, "%s: GroupSep too long (%d bytes)", tag, len(s.GroupSep))
	}
}

// TestNBSPGroupSeparators pins the locales whose CLDR group separator is
// U+00A0. A future edit "fixing" one to a plain space is a regression, and an
// invisible character is exactly the kind of thing a reviewer cannot see.
func TestNBSPGroupSeparators(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{"fr-FR", "pl-PL", "cs-CZ", "uk-UA", "ru-RU", "pt-PT"} {
		s, ok := cldr.FormatFor(tag)
		require.Truef(t, ok, "no spec for %s", tag)
		assert.Equalf(t, "\u00a0", s.GroupSep, "%s GroupSep must be U+00A0 NO-BREAK SPACE, not %q", tag, s.GroupSep)
	}
	// And the ones that are genuinely a plain separator.
	s, _ := cldr.FormatFor("en-US")
	assert.Equal(t, ",", s.GroupSep)
	s, _ = cldr.FormatFor("de-DE")
	assert.Equal(t, ".", s.GroupSep)
	s, _ = cldr.FormatFor("pt-BR")
	assert.Equal(t, ".", s.GroupSep, "pt-BR keeps the pt-root period group, unlike pt-PT's NBSP override")
}

func TestLatAmFormats(t *testing.T) {
	t.Parallel()
	// LatAm is a formats story, not a plurals one: es-MX and es-ES pluralize
	// identically but format nothing alike.
	mx, ok := cldr.FormatFor("es-MX")
	require.True(t, ok)
	assert.Equal(t, ".", mx.DecimalSep)
	assert.Equal(t, ",", mx.GroupSep)
	assert.True(t, mx.CurrencyBefore)
	assert.False(t, mx.CurrencySpace)

	es, ok := cldr.FormatFor("es-ES")
	require.True(t, ok)
	assert.Equal(t, ",", es.DecimalSep)
	assert.Equal(t, ".", es.GroupSep)
	assert.False(t, es.CurrencyBefore)

	// es-AR/es-CO share es-ES's separators but not its currency placement or
	// clock: symbol-first with a space, 12-hour.
	for _, tag := range []string{"es-AR", "es-CO"} {
		s, ok := cldr.FormatFor(tag)
		require.Truef(t, ok, "no spec for %s", tag)
		assert.Equalf(t, ",", s.DecimalSep, "%s", tag)
		assert.Equalf(t, ".", s.GroupSep, "%s", tag)
		assert.Truef(t, s.CurrencyBefore, "%s", tag)
		assert.Truef(t, s.CurrencySpace, "%s", tag)
		assert.Equalf(t, "3:04 PM", s.TimeLayout, "%s", tag)
	}

	// es-CL is the odd one out: hyphenated date, no currency space.
	cl, ok := cldr.FormatFor("es-CL")
	require.True(t, ok)
	assert.Equal(t, "02-01-2006", cl.DateLayout)
	assert.True(t, cl.CurrencyBefore)
	assert.False(t, cl.CurrencySpace)
}

func TestFormatForUnknown(t *testing.T) {
	t.Parallel()
	_, ok := cldr.FormatFor("ww-WW")
	assert.False(t, ok)
	_, ok = cldr.FormatFor("")
	assert.False(t, ok)
	// Specs are per-tag: a bare language has no spec.
	_, ok = cldr.FormatFor("es")
	assert.False(t, ok, "there is no such thing as generic Spanish formatting")
}

// TestFormatForNormalizesInput exercises the underscore and case handling
// FormatFor does on its own, independent of i18n's normalizeTag.
func TestFormatForNormalizesInput(t *testing.T) {
	t.Parallel()
	want, ok := cldr.FormatFor("es-MX")
	require.True(t, ok)

	got, ok := cldr.FormatFor("es_mx")
	require.True(t, ok)
	assert.Equal(t, want, got)

	got, ok = cldr.FormatFor("ES-mx")
	require.True(t, ok)
	assert.Equal(t, want, got)
}

// TestFormatsRenderThroughBundle proves the specs are usable as plain data:
// wire one and the engines in core/i18n do the rest.
func TestFormatsRenderThroughBundle(t *testing.T) {
	t.Parallel()
	b, err := i18n.New(
		i18n.WithTranslations("en", "app", map[string]any{"x": "y"}),
		i18n.WithFormat("en", cldr.FormatDeDE), // arbitrary pairing: it is just data
	)
	require.NoError(t, err)
	assert.Equal(t, "1.234.567,89", b.Number(b.Default(), 1234567.89))
}

func TestFormatsRenderNBSP(t *testing.T) {
	t.Parallel()
	b, err := i18n.New(
		i18n.WithTranslations("fr", "app", map[string]any{"x": "y"}),
		i18n.WithConfig(i18n.Config{DefaultLocale: "fr", CookieName: "lang", QueryParam: "lang"}),
		i18n.WithFormat("fr", cldr.FormatFrFR),
	)
	require.NoError(t, err)
	// The separator here is U+00A0, written as an escape so it is visible.
	assert.Equal(t, "1\u00a0234\u00a0567,89", b.Number(b.Default(), 1234567.89))
}

// renderCase is one locale's expected rendering across every engine that
// consumes a FormatSpec. Expected strings are written from CLDR/locale
// knowledge, independent of the spec's own field values, so a transposed
// separator or a wrong currency placement in format.go fails the assertion
// rather than restating it.
type renderCase struct {
	tag      string
	spec     i18n.FormatSpec
	number   string // 1234567.89
	currency string // 1234.56, symbol "$"
	percent  string // 0.5
	date     string // reference time 2006-01-02
	clock    string // reference time 15:04:05
	dateTime string
}

var renderCases = []renderCase{
	{"en-US", cldr.FormatEnUS, "1,234,567.89", "$1,234.56", "50%", "01/02/2006", "3:04 PM", "01/02/2006 3:04 PM"},
	{"en-GB", cldr.FormatEnGB, "1,234,567.89", "$1,234.56", "50%", "02/01/2006", "15:04", "02/01/2006 15:04"},
	{"de-DE", cldr.FormatDeDE, "1.234.567,89", "1.234,56 $", "50 %", "02.01.2006", "15:04", "02.01.2006 15:04"},
	{"fr-FR", cldr.FormatFrFR, "1\u00a0234\u00a0567,89", "1\u00a0234,56 $", "50 %", "02/01/2006", "15:04", "02/01/2006 15:04"},
	{"es-ES", cldr.FormatEsES, "1.234.567,89", "1.234,56 $", "50 %", "02/01/2006", "15:04", "02/01/2006 15:04"},
	{"es-MX", cldr.FormatEsMX, "1,234,567.89", "$1,234.56", "50%", "02/01/2006", "3:04 PM", "02/01/2006 3:04 PM"},
	{"es-AR", cldr.FormatEsAR, "1.234.567,89", "$ 1.234,56", "50%", "02/01/2006", "3:04 PM", "02/01/2006 3:04 PM"},
	{"es-CO", cldr.FormatEsCO, "1.234.567,89", "$ 1.234,56", "50%", "02/01/2006", "3:04 PM", "02/01/2006 3:04 PM"},
	{"es-CL", cldr.FormatEsCL, "1.234.567,89", "$1.234,56", "50%", "02-01-2006", "3:04 PM", "02-01-2006 3:04 PM"},
	{"it-IT", cldr.FormatItIT, "1.234.567,89", "1.234,56 $", "50%", "02/01/2006", "15:04", "02/01/2006 15:04"},
	{"pt-BR", cldr.FormatPtBR, "1.234.567,89", "$ 1.234,56", "50%", "02/01/2006", "15:04", "02/01/2006 15:04"},
	{"pt-PT", cldr.FormatPtPT, "1\u00a0234\u00a0567,89", "1\u00a0234,56 $", "50%", "02/01/2006", "15:04", "02/01/2006 15:04"},
	{"nl-NL", cldr.FormatNlNL, "1.234.567,89", "$ 1.234,56", "50%", "02-01-2006", "15:04", "02-01-2006 15:04"},
	{"pl-PL", cldr.FormatPlPL, "1\u00a0234\u00a0567,89", "1\u00a0234,56 $", "50%", "02.01.2006", "15:04", "02.01.2006 15:04"},
	{"cs-CZ", cldr.FormatCsCZ, "1\u00a0234\u00a0567,89", "1\u00a0234,56 $", "50 %", "02.01.2006", "15:04", "02.01.2006 15:04"},
	{"uk-UA", cldr.FormatUkUA, "1\u00a0234\u00a0567,89", "1\u00a0234,56 $", "50%", "02.01.2006", "15:04", "02.01.2006 15:04"},
	{"ru-RU", cldr.FormatRuRU, "1\u00a0234\u00a0567,89", "1\u00a0234,56 $", "50 %", "02.01.2006", "15:04", "02.01.2006 15:04"},
	{"tr-TR", cldr.FormatTrTR, "1.234.567,89", "$1.234,56", "50%", "02.01.2006", "15:04", "02.01.2006 15:04"},
	{"ar-AE", cldr.FormatArAE, "1,234,567.89", "1,234.56 $", "50%", "02/01/2006", "3:04 PM", "02/01/2006 3:04 PM"},
	{"ja-JP", cldr.FormatJaJP, "1,234,567.89", "$1,234.56", "50%", "2006/01/02", "15:04", "2006/01/02 15:04"},
	{"zh-CN", cldr.FormatZhCN, "1,234,567.89", "$1,234.56", "50%", "2006/1/2", "15:04", "2006/1/2 15:04"},
	{"ko-KR", cldr.FormatKoKR, "1,234,567.89", "$1,234.56", "50%", "2006. 1. 2.", "PM 3:04", "2006. 1. 2. PM 3:04"},
}

// refTime is Go's own reference instant: the layouts below are all-numeric
// (no locale-specific month/weekday names), so formatting this exact instant
// reproduces each locale's DateLayout/TimeLayout/DateTimeLayout string
// verbatim when — and only when — the layout is a well-formed Go layout in
// the locale's actual field order.
var refTime = time.Date(2006, time.January, 2, 15, 4, 5, 0, time.UTC)

// TestFormatsRenderAllLocales wires every shipped spec into one bundle and
// renders a number, a currency amount, a percentage, and a date/time through
// core/i18n's engines, asserting the exact locale-correct string. This is
// the falsifiable check the brief asks for: a wrong separator, a transposed
// date field, or a flipped currency-before flag fails here, not just a
// field-equality assertion against the spec's own data.
func TestFormatsRenderAllLocales(t *testing.T) {
	t.Parallel()
	opts := []i18n.Option{
		i18n.WithConfig(i18n.Config{DefaultLocale: "en-US", CookieName: "lang", QueryParam: "lang"}),
	}
	for _, rc := range renderCases {
		opts = append(opts,
			i18n.WithTranslations(rc.tag, "app", map[string]any{"x": "y"}),
			i18n.WithFormat(rc.tag, rc.spec),
		)
	}
	b, err := i18n.New(opts...)
	require.NoError(t, err)

	dollar := money.Currency{Code: "USD", Num: "840", Symbol: "$", MinorUnits: 2}

	for _, rc := range renderCases {
		t.Run(rc.tag, func(t *testing.T) {
			loc, err := b.Parse(rc.tag)
			require.NoErrorf(t, err, "%s not registered", rc.tag)

			assert.Equalf(t, rc.number, b.Number(loc, 1234567.89), "%s: Number", rc.tag)
			assert.Equalf(t, rc.currency, b.Currency(loc, money.FromMinor(123456, dollar)), "%s: Currency", rc.tag)
			assert.Equalf(t, rc.percent, b.Percent(loc, 0.5), "%s: Percent", rc.tag)
			assert.Equalf(t, rc.date, b.Date(loc, refTime), "%s: Date", rc.tag)
			assert.Equalf(t, rc.clock, b.Time(loc, refTime), "%s: Time", rc.tag)
			assert.Equalf(t, rc.dateTime, b.DateTime(loc, refTime), "%s: DateTime", rc.tag)
		})
	}
}

// TestTurkishPercentGap documents a known FormatSpec limitation surfaced by
// tr-TR: CLDR's Turkish percent pattern is "%50" (sign leads the number).
// FormatSpec/PercentSpace can only place a space before a trailing sign, so
// the shared engine renders "50%" here — not canonically Turkish, but the
// most FormatSpec can express. This pins the actual (non-canonical) behavior
// so a future engine change is a deliberate decision, not a silent diff.
func TestTurkishPercentGap(t *testing.T) {
	t.Parallel()
	b, err := i18n.New(
		i18n.WithTranslations("tr-TR", "app", map[string]any{"x": "y"}),
		i18n.WithConfig(i18n.Config{DefaultLocale: "tr-TR", CookieName: "lang", QueryParam: "lang"}),
		i18n.WithFormat("tr-TR", cldr.FormatTrTR),
	)
	require.NoError(t, err)
	assert.Equal(t, "50%", b.Percent(b.Default(), 0.5))
}
