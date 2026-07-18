package cldr

import (
	"maps"
	"strings"

	"github.com/dmitrymomot/forge/core/i18n"
)

// Format specs, keyed by locale tag. Specs are per-tag, never per-language:
// es-MX and es-ES pluralize identically and format nothing alike, which is why
// there is no "Format Spanish" — every var below carries a full region.
//
// Values are taken from CLDR (unicode-org/cldr-json, main branch) number and
// date-time patterns: decimal/group separators and percent/currency patterns
// from cldr-numbers-full, date/time field order from cldr-dates-full. Where a
// tag has no CLDR override of its own (pt-BR, zh-CN), the value comes from
// the CLDR root that already represents it (CLDR's "pt" root is Brazilian
// Portuguese; its "zh"/"zh-Hans" root is Mainland Chinese).
//
// GroupSep for fr, pl, cs, uk, ru, and pt-PT is U+00A0 NO-BREAK SPACE — CLDR's
// separator for those locales. It is written as the \u00a0 escape on purpose:
// a raw NBSP is invisible in a diff and indistinguishable from a plain space.
// (Current CLDR actually gives French U+202F NARROW NO-BREAK SPACE, not
// U+00A0; this package deliberately uses the plain NBSP for fr too, to keep a
// single, uniform, reviewable escape across every affected locale rather than
// carrying two visually-identical space characters through the source.)
//
// Turkish CLDR percent format places the sign before the number ("%50", not
// "50%"), which FormatSpec has no field to express — PercentSpace only
// controls a space before a trailing sign. FormatTrTR therefore renders "50%"
// via the shared engine, not the canonical Turkish "%50".
//
// Dates follow each locale's CLDR SHORT pattern, which leaves the day and
// month unpadded (en-US "M/d", es-ES and ar-AE "d/M"): those layouts render
// 1/2/2006 and 2/1/2006, not zero-padded. Where a locale's own short pattern
// pads a field, the padding is kept — es-CO, pl, and tr pad the month ("d/MM",
// "d.MM"), and cs-CZ pads both ("dd.MM").
//
// Go's time layout has no unpadded 24-hour hour token — "15" is always two
// digits — so a locale whose CLDR short time is unpadded 24-hour (es-ES and
// cs-CZ: "H:mm") renders the padded "15:04" instead, the closest Go can
// express. This is the same class of gap as the Turkish percent above and the
// Korean AM/PM marker below.
var (
	// FormatEnUS is the CLDR format spec for en-US.
	FormatEnUS = i18n.FormatSpec{
		DecimalSep: ".", GroupSep: ",",
		DateLayout: "1/2/2006", TimeLayout: "3:04 PM", DateTimeLayout: "1/2/2006 3:04 PM",
		CurrencyBefore: true,
	}
	// FormatEnGB is the CLDR format spec for en-GB: day before month, 24-hour
	// clock — otherwise identical separators and currency placement to en-US.
	FormatEnGB = i18n.FormatSpec{
		DecimalSep: ".", GroupSep: ",",
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		CurrencyBefore: true,
	}

	// FormatDeDE is the CLDR format spec for de-DE: comma decimal, period
	// group, symbol after the amount with a space, space before "%".
	FormatDeDE = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: ".",
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		CurrencySpace: true, PercentSpace: true,
	}
	// FormatFrFR is the CLDR format spec for fr-FR. GroupSep is U+00A0 (see
	// the package-level note on the NBSP/NNBSP distinction above).
	FormatFrFR = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0",
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		CurrencySpace: true, PercentSpace: true,
	}

	// FormatEsES is the CLDR format spec for es-ES: comma decimal, period
	// group, symbol after with a space, space before "%". Date is unpadded
	// (CLDR short "d/M/y"); the 24-hour clock renders padded as "15:04" because
	// Go has no unpadded 24-hour hour token, though CLDR's short time is "H:mm".
	FormatEsES = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: ".",
		DateLayout: "2/1/2006", TimeLayout: "15:04", DateTimeLayout: "2/1/2006 15:04",
		CurrencySpace: true, PercentSpace: true,
	}
	// FormatEsMX is the CLDR format spec for es-MX: US-style separators and
	// symbol-first currency, 12-hour clock — nothing like es-ES.
	FormatEsMX = i18n.FormatSpec{
		DecimalSep: ".", GroupSep: ",",
		DateLayout: "02/01/2006", TimeLayout: "3:04 PM", DateTimeLayout: "02/01/2006 3:04 PM",
		CurrencyBefore: true,
	}
	// FormatEsAR is the CLDR format spec for es-AR: es-ES-style separators
	// (comma decimal, period group) but symbol-first currency with a space
	// and a 12-hour clock, like the rest of Spanish-speaking Latin America.
	FormatEsAR = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: ".",
		DateLayout: "2/1/2006", TimeLayout: "3:04 PM", DateTimeLayout: "2/1/2006 3:04 PM",
		CurrencyBefore: true, CurrencySpace: true,
	}
	// FormatEsCO is the CLDR format spec for es-CO: es-AR's currency and clock,
	// but its CLDR short date pads the month while leaving the day unpadded
	// ("d/MM/y"), unlike es-AR's fully-unpadded "d/M/y".
	FormatEsCO = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: ".",
		DateLayout: "2/01/2006", TimeLayout: "3:04 PM", DateTimeLayout: "2/01/2006 3:04 PM",
		CurrencyBefore: true, CurrencySpace: true,
	}
	// FormatEsCL is the CLDR format spec for es-CL: Chile is the one Spanish
	// locale in this set with a hyphenated date (dd-MM-yyyy) and no space
	// between the currency symbol and the amount.
	FormatEsCL = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: ".",
		DateLayout: "02-01-2006", TimeLayout: "3:04 PM", DateTimeLayout: "02-01-2006 3:04 PM",
		CurrencyBefore: true,
	}

	// FormatItIT is the CLDR format spec for it-IT: es-ES-like separators,
	// symbol after the amount with a space, no space before "%", 24-hour
	// clock.
	FormatItIT = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: ".",
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		CurrencySpace: true,
	}

	// FormatPtBR is the CLDR format spec for pt-BR: CLDR's "pt" root already
	// represents Brazilian Portuguese (there is no separate pt-BR override).
	// Symbol first with a space, period group, 24-hour clock.
	FormatPtBR = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: ".",
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		CurrencyBefore: true, CurrencySpace: true,
	}
	// FormatPtPT is the CLDR format spec for pt-PT: a genuine override of the
	// pt root — NBSP group (not period), symbol after the amount, not
	// before. Confirms es-MX's lesson: a region is not its language.
	FormatPtPT = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0",
		DateLayout: "02/01/2006", TimeLayout: "15:04", DateTimeLayout: "02/01/2006 15:04",
		CurrencySpace: true,
	}

	// FormatNlNL is the CLDR format spec for nl-NL: hyphenated date order
	// (dd-MM-yyyy), symbol first with a space, 24-hour clock.
	FormatNlNL = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: ".",
		DateLayout: "02-01-2006", TimeLayout: "15:04", DateTimeLayout: "02-01-2006 15:04",
		CurrencyBefore: true, CurrencySpace: true,
	}

	// FormatPlPL is the CLDR format spec for pl-PL. GroupSep is U+00A0. CLDR
	// short date "d.MM.y" leaves the day unpadded but pads the month.
	FormatPlPL = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0",
		DateLayout: "2.01.2006", TimeLayout: "15:04", DateTimeLayout: "2.01.2006 15:04",
		CurrencySpace: true,
	}
	// FormatCsCZ is the CLDR format spec for cs-CZ. GroupSep is U+00A0.
	FormatCsCZ = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0",
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		CurrencySpace: true, PercentSpace: true,
	}
	// FormatUkUA is the CLDR format spec for uk-UA. GroupSep is U+00A0.
	FormatUkUA = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0",
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		CurrencySpace: true,
	}
	// FormatRuRU is the CLDR format spec for ru-RU. GroupSep is U+00A0.
	FormatRuRU = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: "\u00a0",
		DateLayout: "02.01.2006", TimeLayout: "15:04", DateTimeLayout: "02.01.2006 15:04",
		CurrencySpace: true, PercentSpace: true,
	}

	// FormatTrTR is the CLDR format spec for tr-TR: German-shaped separators
	// (comma decimal, period group), symbol first with no space. CLDR short
	// date "d.MM.y" leaves the day unpadded but pads the month. See the package
	// doc comment for the percent-sign-placement gap this locale hits.
	FormatTrTR = i18n.FormatSpec{
		DecimalSep: ",", GroupSep: ".",
		DateLayout: "2.01.2006", TimeLayout: "15:04", DateTimeLayout: "2.01.2006 15:04",
		CurrencyBefore: true,
	}

	// FormatArAE is the CLDR format spec for ar-AE. The UAE's default
	// numbering system is Western (latn), not Eastern Arabic-Indic digits, so
	// English-shaped separators apply; the currency symbol trails the amount
	// with a space, and the clock is 12-hour. CLDR short date "d/M/y" is
	// unpadded (the RTL marks CLDR interleaves are dropped: these layouts carry
	// digits and plain "/", and directionality is the renderer's job).
	FormatArAE = i18n.FormatSpec{
		DecimalSep: ".", GroupSep: ",",
		DateLayout: "2/1/2006", TimeLayout: "3:04 PM", DateTimeLayout: "2/1/2006 3:04 PM",
		CurrencySpace: true,
	}

	// FormatJaJP is the CLDR format spec for ja-JP: year-month-day order,
	// English-shaped separators, symbol first, 24-hour clock.
	FormatJaJP = i18n.FormatSpec{
		DecimalSep: ".", GroupSep: ",",
		DateLayout: "2006/01/02", TimeLayout: "15:04", DateTimeLayout: "2006/01/02 15:04",
		CurrencyBefore: true,
	}
	// FormatZhCN is the CLDR format spec for zh-CN: CLDR's "zh"/"zh-Hans"
	// root already represents Mainland China (there is no separate zh-CN
	// override). Year-month-day order, symbol first — but unlike ja-JP, the
	// month and day are UNPADDED (CLDR short date pattern "y/M/d", not
	// "y/MM/dd").
	FormatZhCN = i18n.FormatSpec{
		DecimalSep: ".", GroupSep: ",",
		DateLayout: "2006/1/2", TimeLayout: "15:04", DateTimeLayout: "2006/1/2 15:04",
		CurrencyBefore: true,
	}
	// FormatKoKR is the CLDR format spec for ko-KR: year-month-day order with
	// a trailing period on each field, month/day UNPADDED (CLDR medium date
	// pattern "y. M. d."), and the AM/PM marker leads the clock time rather
	// than trailing it ("PM 3:04", not "3:04 PM"). Go's AM/PM token itself
	// only ever renders the literal English "AM"/"PM".
	FormatKoKR = i18n.FormatSpec{
		DecimalSep: ".", GroupSep: ",",
		DateLayout: "2006. 1. 2.", TimeLayout: "PM 3:04", DateTimeLayout: "2006. 1. 2. PM 3:04",
		CurrencyBefore: true,
	}
)

// allFormats maps locale tag to spec. Keep in sync with the vars above; the
// TestFormatSpecsAreComplete test pins the full set.
var allFormats = map[string]i18n.FormatSpec{
	"en-US": FormatEnUS, "en-GB": FormatEnGB,
	"de-DE": FormatDeDE, "fr-FR": FormatFrFR,
	"es-ES": FormatEsES, "es-MX": FormatEsMX, "es-AR": FormatEsAR, "es-CO": FormatEsCO, "es-CL": FormatEsCL,
	"it-IT": FormatItIT,
	"pt-BR": FormatPtBR, "pt-PT": FormatPtPT,
	"nl-NL": FormatNlNL,
	"pl-PL": FormatPlPL, "cs-CZ": FormatCsCZ, "uk-UA": FormatUkUA, "ru-RU": FormatRuRU,
	"tr-TR": FormatTrTR,
	"ar-AE": FormatArAE,
	"ja-JP": FormatJaJP, "zh-CN": FormatZhCN, "ko-KR": FormatKoKR,
}

// AllFormats returns every spec keyed by locale tag. The returned map is a
// copy; mutating it affects nothing.
func AllFormats() map[string]i18n.FormatSpec {
	out := make(map[string]i18n.FormatSpec, len(allFormats))
	maps.Copy(out, allFormats)
	return out
}

// FormatFor looks up a spec by locale tag. ok is false for a tag this package
// does not carry — the caller then leaves it unwired and core/i18n's
// Invariant applies. Lookup is by exact tag: there is no base-language
// fallback, because a language does not have a number format (there is no
// "Format Spanish").
//
// This is a convenience for callers. core/i18n never calls it.
func FormatFor(tag string) (i18n.FormatSpec, bool) {
	t := strings.ReplaceAll(strings.TrimSpace(tag), "_", "-")
	lang, region, ok := strings.Cut(t, "-")
	if !ok {
		return i18n.FormatSpec{}, false
	}
	s, found := allFormats[strings.ToLower(lang)+"-"+strings.ToUpper(region)]
	return s, found
}
