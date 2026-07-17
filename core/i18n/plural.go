package i18n

// PluralCategory is a CLDR plural category. Not every language uses every
// category; unused forms in a catalog fall back along formFallback chains.
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

// String returns the CLDR category name ("zero" … "other").
func (c PluralCategory) String() string {
	if int(c) < len(categoryNames) {
		return categoryNames[c]
	}
	return "other"
}

// categoryByName maps catalog sub-key names to categories. Returns ok=false
// for non-plural sub-keys.
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

// PluralRule selects the plural category for an integer count. Rules are
// curated per language in plural_data.go; WithPlural overrides or adds them.
type PluralRule func(n int) PluralCategory

// fallbackChains defines, per category, which forms to try when a catalog
// lacks the selected form (spec: two→few→many→other, few→many→other,
// zero→other, many→other, one→other).
var fallbackChains = [numCategories][]PluralCategory{
	Zero: {Other},
	One:  {Other},
	Two:  {Few, Many, Other},
	Few:  {Many, Other},
	Many: {Other},
}

func formFallback(c PluralCategory) []PluralCategory {
	if int(c) < len(fallbackChains) {
		return fallbackChains[c]
	}
	return nil
}

// pluralProbeNumbers exercises the interesting bands of every curated rule
// (Slavic mod-10/mod-100 windows, Arabic bands, millions).
var pluralProbeNumbers = []int{0, 1, 2, 3, 4, 5, 10, 11, 12, 13, 14, 19, 20, 21, 22, 25, 100, 101, 102, 111, 1000, 1000000}

// SupportedForms reports which plural categories a rule can produce, probed
// over representative counts. Used for load-time catalog validation.
func SupportedForms(rule PluralRule) []PluralCategory {
	var seen [numCategories]bool
	for _, n := range pluralProbeNumbers {
		seen[rule(n)] = true
	}
	out := make([]PluralCategory, 0, numCategories)
	for c := range PluralCategory(numCategories) {
		if seen[c] {
			out = append(out, c)
		}
	}
	return out
}
