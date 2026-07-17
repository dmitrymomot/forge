package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ruleByLang mirrors locale_data.go's per-row wiring so table cases stay
// readable by language. Test-local on purpose: production code names rules
// directly and must not grow a lang→rule mapper (it would be dead code).
var ruleByLang = map[string]PluralRule{
	"en": ruleOneOther, "de": ruleOneOther, "nl": ruleOneOther, "tr": ruleOneOther,
	"fr": ruleFrench, "es": ruleSpanish,
	"it": ruleItalianPortuguese, "pt": ruleItalianPortuguese,
	"uk": ruleEastSlavic, "ru": ruleEastSlavic,
	"pl": rulePolish, "cs": ruleCzech, "ar": ruleArabic,
	"ja": ruleOther, "zh": ruleOther,
}

func TestPluralRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		lang string
		n    int
		want PluralCategory
	}{
		// English/Germanic/Turkic: one/other, 0 → other (no CLDR zero).
		{"en", 0, Other}, {"en", 1, One}, {"en", -1, One}, {"en", 2, Other}, {"en", 21, Other},
		{"de", 1, One}, {"de", 0, Other}, {"nl", 1, One}, {"tr", 1, One}, {"tr", 0, Other},
		// French: 0 and 1 → one; 1e6 → many.
		{"fr", 0, One}, {"fr", 1, One}, {"fr", 2, Other}, {"fr", 1000000, Many},
		// Spanish: 1 → one; 1e6 → many; 0 → other.
		{"es", 0, Other}, {"es", 1, One}, {"es", 1000000, Many},
		// Italian/Portuguese: 1 → one; 1e6 → many.
		{"it", 1, One}, {"it", 2, Other}, {"it", 1000000, Many},
		{"pt", 1, One}, {"pt", 0, Other}, {"pt", 1000000, Many},
		// East Slavic (uk/ru): 21 → one (the old SlavicPluralRule bug), 11 → many, 0 → many.
		{"uk", 1, One}, {"uk", 21, One}, {"uk", 101, One},
		{"uk", 2, Few}, {"uk", 4, Few}, {"uk", 22, Few}, {"uk", 102, Few},
		{"uk", 0, Many}, {"uk", 5, Many}, {"uk", 11, Many}, {"uk", 12, Many}, {"uk", 14, Many}, {"uk", 111, Many},
		{"ru", 21, One}, {"ru", 12, Many}, {"ru", 25, Many},
		// Polish: one only for exactly 1; 21 → many (differs from uk/ru).
		{"pl", 1, One}, {"pl", 21, Many}, {"pl", 2, Few}, {"pl", 22, Few}, {"pl", 12, Many}, {"pl", 0, Many},
		// Czech: 1 → one, 2–4 → few, else other.
		{"cs", 1, One}, {"cs", 2, Few}, {"cs", 4, Few}, {"cs", 5, Other}, {"cs", 0, Other}, {"cs", 12, Other},
		// Arabic bands.
		{"ar", 0, Zero}, {"ar", 1, One}, {"ar", 2, Two}, {"ar", 3, Few}, {"ar", 10, Few},
		{"ar", 103, Few}, {"ar", 11, Many}, {"ar", 99, Many}, {"ar", 100, Other}, {"ar", 102, Other},
		// CJK & friends: other only.
		{"ja", 1, Other}, {"zh", 1, Other},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ruleByLang[c.lang](c.n), "%s(%d)", c.lang, c.n)
	}
}

func TestPluralRulesNegative(t *testing.T) {
	t.Parallel()
	assert.Equal(t, One, ruleEastSlavic(-21), "uk(-21)")
	assert.Equal(t, Few, rulePolish(-3), "pl(-3)")
}
