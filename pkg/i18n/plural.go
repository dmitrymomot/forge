package i18n

import "strings"

// PluralRule determines which plural form to use for a given count.
// It follows Unicode CLDR (Common Locale Data Repository) guidelines.
type PluralRule func(n int) string

// Plural category constants as defined by Unicode CLDR.
// Not all languages use all categories.
const (
	PluralZero  = "zero"
	PluralOne   = "one"
	PluralTwo   = "two"
	PluralFew   = "few"
	PluralMany  = "many"
	PluralOther = "other"
)

// DefaultPluralRule provides a generic plural rule that works reasonably
// well for languages without specific rules. It distinguishes between
// zero, one, few, many, and other.
var DefaultPluralRule PluralRule = func(n int) string {
	if n == 0 {
		return PluralZero
	}

	absN := n
	if n < 0 {
		absN = -n
	}

	if absN == 1 {
		return PluralOne
	}
	if absN >= 2 && absN <= 4 {
		return PluralFew
	}
	if absN > 4 && absN < 20 {
		return PluralMany
	}
	return PluralOther
}

// EnglishPluralRule implements plural rules for English and similar languages.
// Categories: zero (0), one (1), other (everything else)
var EnglishPluralRule PluralRule = func(n int) string {
	if n == 0 {
		return PluralZero
	}
	if n == 1 || n == -1 {
		return PluralOne
	}
	return PluralOther
}

// SlavicPluralRule implements plural rules for Slavic languages
// (Polish, Czech, Ukrainian, Croatian, Serbian, etc.)
// Categories: zero, one, few, many
var SlavicPluralRule PluralRule = func(n int) string {
	if n == 0 {
		return PluralZero
	}
	if n == 1 || n == -1 {
		return PluralOne
	}

	absN := n
	if n < 0 {
		absN = -n
	}

	mod10 := absN % 10
	mod100 := absN % 100

	if mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14) {
		return PluralFew
	}

	return PluralMany
}

// RomancePluralRule implements plural rules for Romance languages
// (French, Italian, Portuguese, but NOT Spanish which is simpler)
// Categories: one (0, 1), many (1,000,000+), other
var RomancePluralRule PluralRule = func(n int) string {
	if n == 0 || n == 1 || n == -1 {
		return PluralOne
	}
	absN := n
	if n < 0 {
		absN = -n
	}
	if absN >= 1000000 {
		return PluralMany
	}
	return PluralOther
}

// GermanicPluralRule implements plural rules for Germanic languages
// (German, Dutch, Swedish, Norwegian, Danish)
// Categories: one (1), other (everything else including 0)
var GermanicPluralRule PluralRule = func(n int) string {
	if n == 1 || n == -1 {
		return PluralOne
	}
	return PluralOther
}

// AsianPluralRule implements plural rules for Asian languages
// that don't distinguish plural forms
// (Japanese, Chinese, Korean, Thai, Vietnamese)
// Categories: other (all numbers)
var AsianPluralRule PluralRule = func(_ int) string {
	return PluralOther
}

// ArabicPluralRule implements complex plural rules for Arabic.
// Categories: zero, one, two, few, many, other
var ArabicPluralRule PluralRule = func(n int) string {
	if n == 0 {
		return PluralZero
	}
	if n == 1 || n == -1 {
		return PluralOne
	}
	if n == 2 || n == -2 {
		return PluralTwo
	}

	absN := n
	if n < 0 {
		absN = -n
	}

	mod100 := absN % 100

	if mod100 >= 3 && mod100 <= 10 {
		return PluralFew
	}

	if mod100 >= 11 && mod100 <= 99 {
		return PluralMany
	}

	return PluralOther
}

// SpanishPluralRule implements plural rules for Spanish.
// Simpler than other Romance languages.
// Categories: one (1), many (1,000,000+), other
var SpanishPluralRule PluralRule = func(n int) string {
	if n == 1 || n == -1 {
		return PluralOne
	}
	absN := n
	if n < 0 {
		absN = -n
	}
	if absN >= 1000000 {
		return PluralMany
	}
	return PluralOther
}

// GetPluralRuleForLanguage returns the appropriate plural rule for a given language code.
// It uses the two-letter ISO 639-1 language code (e.g., "en", "fr", "pl").
// Falls back to DefaultPluralRule for unknown languages.
func GetPluralRuleForLanguage(lang string) PluralRule {
	if len(lang) >= 2 {
		lang = strings.ToLower(lang[:2])
	}

	switch lang {
	case "en":
		return EnglishPluralRule
	case "pl", "ru", "cs", "uk", "hr", "sr", "sk", "sl", "bg":
		return SlavicPluralRule
	case "fr", "it", "pt":
		return RomancePluralRule
	case "es":
		return SpanishPluralRule
	case "de", "nl", "sv", "no", "da", "is":
		return GermanicPluralRule
	case "ja", "zh", "ko", "th", "vi", "id", "ms":
		return AsianPluralRule
	case "ar":
		return ArabicPluralRule
	default:
		return DefaultPluralRule
	}
}

// SupportedPluralForms returns which plural forms a rule actually uses.
// This is useful for validation when loading translations.
func SupportedPluralForms(rule PluralRule) []string {
	forms := make(map[string]bool)

	testNumbers := []int{0, 1, 2, 3, 4, 5, 10, 11, 12, 13, 14, 20, 21, 22, 100, 1000, 1000000}

	for _, n := range testNumbers {
		form := rule(n)
		forms[form] = true
	}

	order := []string{PluralZero, PluralOne, PluralTwo, PluralFew, PluralMany, PluralOther}
	var result []string
	for _, form := range order {
		if forms[form] {
			result = append(result, form)
		}
	}

	return result
}
