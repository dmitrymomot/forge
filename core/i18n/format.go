package i18n

// FormatSpec holds one locale's rendering conventions. This package ships no
// per-locale values — only Invariant. Real CLDR specs live in core/i18n/cldr
// and are wired with WithFormat.
type FormatSpec struct {
	// DecimalSep separates integer and fractional digits ("." or ",").
	DecimalSep string
	// GroupSep separates 3-digit groups ("," "." or U+00A0).
	GroupSep string
	// DateLayout, TimeLayout, DateTimeLayout are Go time layouts.
	DateLayout     string
	TimeLayout     string
	DateTimeLayout string
	// CurrencyBefore places the symbol before the amount ($1.50 vs 1,50 €).
	CurrencyBefore bool
	// CurrencySpace inserts a space between symbol and amount.
	CurrencySpace bool
	// PercentSpace inserts a space before the percent sign (fr: "50 %").
	PercentSpace bool
}

// Invariant is the FormatSpec applied to any locale with no wired spec: a
// neutral, ISO-8601 rendering. It is not a claim that any locale looks like
// this — it is the honest default for a package that knows no locales.
var Invariant = FormatSpec{
	DecimalSep:     ".",
	GroupSep:       ",",
	DateLayout:     "2006-01-02",
	TimeLayout:     "15:04",
	DateTimeLayout: "2006-01-02 15:04",
	CurrencyBefore: true,
}
