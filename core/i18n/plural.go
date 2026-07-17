package i18n

// PluralCategory is a CLDR plural category. Not every language uses every
// category; unused forms in a catalog resolve along formFallback chains.
type PluralCategory uint8

// CLDR plural categories in canonical order. The numeric values index
// per-message form arrays and must stay dense and stable.
const (
	Zero PluralCategory = iota
	One
	Two
	Few
	Many
	Other

	numCategories = 6
)

var categoryNames = [numCategories]string{"zero", "one", "two", "few", "many", "other"}

// String returns the CLDR category name ("zero" … "other"). Out-of-range
// values report "other", the terminal form — String never panics.
func (c PluralCategory) String() string {
	if int(c) < len(categoryNames) {
		return categoryNames[c]
	}
	return "other"
}

// categoryByName maps a catalog sub-key to a category. ok is false for any
// sub-key that is not a plural form, which is how catalog.go tells a plural
// message apart from a nested namespace.
func categoryByName(s string) (PluralCategory, bool) {
	switch s {
	case "zero":
		return Zero, true
	case "one":
		return One, true
	case "two":
		return Two, true
	case "few":
		return Few, true
	case "many":
		return Many, true
	case "other":
		return Other, true
	}
	return 0, false
}

// PluralRule selects the plural category for an integer count. This package
// ships exactly one rule — DefaultRule — and knows nothing about any
// language's grammar. Real CLDR rules live in core/i18n/cldr and are wired
// per language via WithPlural.
type PluralRule func(n int) PluralCategory

// DefaultRule is the built-in fallback rule applied to any locale for which
// no rule was wired: zero for 0, one for ±1, many otherwise.
//
// It is deliberately not any real language's rule. It exists so that T is
// total without the package having to guess at grammar, and it composes with
// the form-fallback chain to do the right thing for an ordinary {one, other}
// catalog (many→other, zero→other) while giving a catalog that defines a
// zero form a working zero for free.
//
// It compares against ±1 directly rather than taking an absolute value: abs
// overflows at math.MinInt, and DefaultRule must be total for every int.
func DefaultRule(n int) PluralCategory {
	switch n {
	case 0:
		return Zero
	case 1, -1:
		return One
	default:
		return Many
	}
}

// fallbackChains defines, per category, which forms to try when a catalog
// lacks the selected form. Other is the terminal form and has no chain.
var fallbackChains = [numCategories][]PluralCategory{
	Zero: {Other},
	One:  {Other},
	Two:  {Few, Many, Other},
	Few:  {Many, Other},
	Many: {Other},
}

// formFallback reports which forms to try, in order, when a catalog lacks
// the category c. It returns nil for Other (the terminal form) and for any
// out-of-range category.
func formFallback(c PluralCategory) []PluralCategory {
	if int(c) < len(fallbackChains) {
		return fallbackChains[c]
	}
	return nil
}

// pluralProbeNumbers exercises the bands real CLDR rules discriminate on:
// Slavic mod-10/mod-100 windows, Arabic bands, and whole millions.
var pluralProbeNumbers = []int{0, 1, 2, 3, 4, 5, 10, 11, 12, 13, 14, 19, 20, 21, 22, 25, 100, 101, 102, 111, 1000, 1000000}

// SupportedForms reports which plural categories a rule can produce, probed
// over representative counts. Bundle uses it at construction to report
// incomplete or dead translations through the miss handler. Results are in
// canonical category order.
func SupportedForms(rule PluralRule) []PluralCategory {
	var seen [numCategories]bool
	for _, n := range pluralProbeNumbers {
		if c := rule(n); int(c) < numCategories {
			seen[c] = true
		}
	}
	out := make([]PluralCategory, 0, numCategories)
	for c := range PluralCategory(numCategories) {
		if seen[c] {
			out = append(out, c)
		}
	}
	return out
}
