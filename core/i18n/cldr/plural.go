package cldr

import (
	"maps"
	"strings"

	"github.com/dmitrymomot/forge/core/i18n"
)

// magnitude returns |n| as a uint64. It exists because -math.MinInt overflows
// back to math.MinInt: every rule here must be total for every int.
func magnitude(n int) uint64 {
	if n < 0 {
		return uint64(-(n + 1)) + 1
	}
	return uint64(n)
}

// --- shared rule bodies -----------------------------------------------------

// oneOther: one for exactly 1 (or -1), other otherwise. The simple two-form
// shape shared by Germanic, Uralic, and several unrelated languages whose
// CLDR cardinal rule draws no further distinction.
func oneOther(n int) i18n.PluralCategory {
	if n == 1 || n == -1 {
		return i18n.One
	}
	return i18n.Other
}

// otherOnly: no plural distinction (CJK, Thai, Vietnamese, Malay/Indonesian,
// Korean).
func otherOnly(int) i18n.PluralCategory { return i18n.Other }

// romanceZeroOne: one covers 0 and 1. French and Portuguese share this at the
// low end; both also gained the CLDR 38 whole-millions "many" category.
func romanceZeroOne(n int) i18n.PluralCategory {
	a := magnitude(n)
	if a == 0 || a == 1 {
		return i18n.One
	}
	if a >= 1000000 && a%1000000 == 0 {
		return i18n.Many // CLDR 38+
	}
	return i18n.Other
}

// romanceOne: one for exactly 1. Spanish, Italian, and Catalan share this,
// plus the CLDR 38 whole-millions "many" category.
func romanceOne(n int) i18n.PluralCategory {
	a := magnitude(n)
	if a == 1 {
		return i18n.One
	}
	if a >= 1000000 && a%1000000 == 0 {
		return i18n.Many // CLDR 38+
	}
	return i18n.Other
}

// romanian: one for exactly 1; few for 0 and for the n%100 in 1..19 window
// (excluding 1 itself, already claimed by one); other otherwise.
func romanian(n int) i18n.PluralCategory {
	a := magnitude(n)
	if a == 1 {
		return i18n.One
	}
	m100 := a % 100
	if a == 0 || (m100 >= 1 && m100 <= 19) {
		return i18n.Few
	}
	return i18n.Other
}

// eastSlavic: one for n%10==1 && n%100!=11; few for n%10 in 2..4 outside
// 12..14; many otherwise (including 0). Russian, Ukrainian, and Belarusian
// share this shape.
func eastSlavic(n int) i18n.PluralCategory {
	a := magnitude(n)
	m10, m100 := a%10, a%100
	switch {
	case m10 == 1 && m100 != 11:
		return i18n.One
	case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
		return i18n.Few
	default:
		return i18n.Many
	}
}

// southSlavic: same one/few windows as eastSlavic, but the default is other,
// not many — Croatian and Serbian have no "many" cardinal category.
func southSlavic(n int) i18n.PluralCategory {
	a := magnitude(n)
	m10, m100 := a%10, a%100
	switch {
	case m10 == 1 && m100 != 11:
		return i18n.One
	case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
		return i18n.Few
	default:
		return i18n.Other
	}
}

// westSlavic4: one for exactly 1; few for 2..4; other otherwise. Czech and
// Slovak also carry a "many" for fractions, which the integer branch never
// reaches.
func westSlavic4(n int) i18n.PluralCategory {
	a := magnitude(n)
	switch {
	case a == 1:
		return i18n.One
	case a >= 2 && a <= 4:
		return i18n.Few
	default:
		return i18n.Other
	}
}

// polish: one only for exactly 1; few for the n%10 in 2..4 window outside
// 12..14; many otherwise. Note 21 is many here and one in East Slavic — this
// is exactly why these are separate functions.
func polish(n int) i18n.PluralCategory {
	a := magnitude(n)
	if a == 1 {
		return i18n.One
	}
	m10, m100 := a%10, a%100
	if m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) {
		return i18n.Few
	}
	return i18n.Many
}

// slovenian: mod-100 windows into one/two/few, a shape no other language
// here shares.
func slovenian(n int) i18n.PluralCategory {
	a := magnitude(n)
	switch a % 100 {
	case 1:
		return i18n.One
	case 2:
		return i18n.Two
	case 3, 4:
		return i18n.Few
	default:
		return i18n.Other
	}
}

// mod10OneExcept11: one when n%10==1 except when n%100==11; other otherwise.
// Icelandic's shape.
func mod10OneExcept11(n int) i18n.PluralCategory {
	a := magnitude(n)
	if a%10 == 1 && a%100 != 11 {
		return i18n.One
	}
	return i18n.Other
}

// latvian: zero for n%10==0 or n%100 in 11..19; one for n%10==1 excluding
// n%100==11; other otherwise.
func latvian(n int) i18n.PluralCategory {
	a := magnitude(n)
	m10, m100 := a%10, a%100
	switch {
	case m10 == 0 || (m100 >= 11 && m100 <= 19):
		return i18n.Zero
	case m10 == 1 && m100 != 11:
		return i18n.One
	default:
		return i18n.Other
	}
}

// lithuanian: one for n%10==1 outside 11..19; few for n%10 in 2..9 outside
// 11..19; other otherwise.
func lithuanian(n int) i18n.PluralCategory {
	a := magnitude(n)
	m10, m100 := a%10, a%100
	switch {
	case m10 == 1 && (m100 < 11 || m100 > 19):
		return i18n.One
	case m10 >= 2 && m10 <= 9 && (m100 < 11 || m100 > 19):
		return i18n.Few
	default:
		return i18n.Other
	}
}

// hindi: one covers 0 and 1, with no whole-millions "many" (unlike the
// Romance languages that share the same low-end shape).
func hindi(n int) i18n.PluralCategory {
	a := magnitude(n)
	if a == 0 || a == 1 {
		return i18n.One
	}
	return i18n.Other
}

// arabic: the full six-category rule.
func arabic(n int) i18n.PluralCategory {
	a := magnitude(n)
	switch a {
	case 0:
		return i18n.Zero
	case 1:
		return i18n.One
	case 2:
		return i18n.Two
	}
	m100 := a % 100
	switch {
	case m100 >= 3 && m100 <= 10:
		return i18n.Few
	case m100 >= 11 && m100 <= 99:
		return i18n.Many
	default:
		return i18n.Other
	}
}

// hebrew: one for 1, two for 2, other otherwise. Hebrew has no "zero"
// cardinal category, and CLDR removed the round-tens "many" category at v42.
func hebrew(n int) i18n.PluralCategory {
	a := magnitude(n)
	switch a {
	case 1:
		return i18n.One
	case 2:
		return i18n.Two
	default:
		return i18n.Other
	}
}

// --- per-language rules -----------------------------------------------------
// One exported var per language. Never a family bucket: sharing a body is
// fine, sharing an identifier is how uk/ru 21 and pt/it 0 got broken before.

var (
	// En is the CLDR cardinal rule for English.
	En i18n.PluralRule = oneOther
	// De is the CLDR cardinal rule for German.
	De i18n.PluralRule = oneOther
	// Nl is the CLDR cardinal rule for Dutch.
	Nl i18n.PluralRule = oneOther
	// Sv is the CLDR cardinal rule for Swedish.
	Sv i18n.PluralRule = oneOther
	// Da is the CLDR cardinal rule for Danish.
	Da i18n.PluralRule = oneOther
	// Nb is the CLDR cardinal rule for Norwegian Bokmål.
	Nb i18n.PluralRule = oneOther
	// Is is the CLDR cardinal rule for Icelandic: one is n%10==1 except
	// n%100==11, so 21, 31, ... are one too, unlike English.
	Is i18n.PluralRule = mod10OneExcept11

	// Fr is the CLDR cardinal rule for French: one covers 0..1.
	Fr i18n.PluralRule = romanceZeroOne
	// Es is the CLDR cardinal rule for Spanish: one is exactly 1.
	Es i18n.PluralRule = romanceOne
	// It is the CLDR cardinal rule for Italian: one is exactly 1.
	It i18n.PluralRule = romanceOne
	// Pt is the CLDR cardinal rule for Portuguese: one covers 0..1, which is
	// where it diverges from Italian and Spanish.
	Pt i18n.PluralRule = romanceZeroOne
	// Ro is the CLDR cardinal rule for Romanian: one, few (0 and n%100 in
	// 1..19), other.
	Ro i18n.PluralRule = romanian
	// Ca is the CLDR cardinal rule for Catalan: one is exactly 1.
	Ca i18n.PluralRule = romanceOne
	// Gl is the CLDR cardinal rule for Galician: one is exactly 1, with no
	// whole-millions many, unlike its Romance relatives above.
	Gl i18n.PluralRule = oneOther

	// Ru is the CLDR cardinal rule for Russian.
	Ru i18n.PluralRule = eastSlavic
	// Uk is the CLDR cardinal rule for Ukrainian.
	Uk i18n.PluralRule = eastSlavic
	// Be is the CLDR cardinal rule for Belarusian.
	Be i18n.PluralRule = eastSlavic
	// Pl is the CLDR cardinal rule for Polish: 21 is many, unlike East
	// Slavic.
	Pl i18n.PluralRule = polish
	// Cs is the CLDR cardinal rule for Czech.
	Cs i18n.PluralRule = westSlavic4
	// Sk is the CLDR cardinal rule for Slovak.
	Sk i18n.PluralRule = westSlavic4
	// Sl is the CLDR cardinal rule for Slovenian: mod-100 one/two/few.
	Sl i18n.PluralRule = slovenian
	// Hr is the CLDR cardinal rule for Croatian: East-Slavic-shaped windows
	// but no many category.
	Hr i18n.PluralRule = southSlavic
	// Sr is the CLDR cardinal rule for Serbian: same shape as Croatian.
	Sr i18n.PluralRule = southSlavic
	// Bg is the CLDR cardinal rule for Bulgarian: one is exactly 1.
	Bg i18n.PluralRule = oneOther
	// Mk is the CLDR cardinal rule for Macedonian: one is n%10==1 except
	// n%100==11 — the same shape as Icelandic.
	Mk i18n.PluralRule = mod10OneExcept11

	// Lv is the CLDR cardinal rule for Latvian: zero/one/other with mod-10
	// and mod-100 windows.
	Lv i18n.PluralRule = latvian
	// Lt is the CLDR cardinal rule for Lithuanian: one/few/other with mod-10
	// and mod-100 windows.
	Lt i18n.PluralRule = lithuanian
	// Fi is the CLDR cardinal rule for Finnish: one is exactly 1.
	Fi i18n.PluralRule = oneOther
	// Et is the CLDR cardinal rule for Estonian: one is exactly 1.
	Et i18n.PluralRule = oneOther
	// Hu is the CLDR cardinal rule for Hungarian: one is exactly 1.
	Hu i18n.PluralRule = oneOther
	// El is the CLDR cardinal rule for Greek: one is exactly 1.
	El i18n.PluralRule = oneOther
	// Sq is the CLDR cardinal rule for Albanian: one is exactly 1.
	Sq i18n.PluralRule = oneOther
	// Tr is the CLDR cardinal rule for Turkish: one is exactly 1.
	Tr i18n.PluralRule = oneOther
	// Eu is the CLDR cardinal rule for Basque: one is exactly 1.
	Eu i18n.PluralRule = oneOther

	// Ja is the CLDR cardinal rule for Japanese: no plural distinction.
	Ja i18n.PluralRule = otherOnly
	// Zh is the CLDR cardinal rule for Chinese: no plural distinction.
	Zh i18n.PluralRule = otherOnly
	// Ko is the CLDR cardinal rule for Korean: no plural distinction.
	Ko i18n.PluralRule = otherOnly
	// Th is the CLDR cardinal rule for Thai: no plural distinction.
	Th i18n.PluralRule = otherOnly
	// Vi is the CLDR cardinal rule for Vietnamese: no plural distinction.
	Vi i18n.PluralRule = otherOnly
	// Id is the CLDR cardinal rule for Indonesian: no plural distinction.
	Id i18n.PluralRule = otherOnly
	// Ms is the CLDR cardinal rule for Malay: no plural distinction.
	Ms i18n.PluralRule = otherOnly
	// Hi is the CLDR cardinal rule for Hindi: one covers 0 and 1, with no
	// whole-millions many.
	Hi i18n.PluralRule = hindi
	// Ar is the CLDR cardinal rule for Arabic: all six categories.
	Ar i18n.PluralRule = arabic
	// He is the CLDR cardinal rule for Hebrew: one, two, other — no zero, and
	// no round-tens "many". CLDR v42 removed Hebrew's "many" category; a
	// translator must not author a dead "many" form (x/text v0.38 still reports
	// it — see the staleExceptions note in plural_test.go).
	He i18n.PluralRule = hebrew
)

// all maps base language to rule. Keep it in sync with the vars above; the
// TestAllIsComplete test pins the full set.
var all = map[string]i18n.PluralRule{
	"en": En, "de": De, "nl": Nl, "sv": Sv, "da": Da, "nb": Nb, "is": Is,
	"fr": Fr, "es": Es, "it": It, "pt": Pt, "ro": Ro, "ca": Ca, "gl": Gl,
	"ru": Ru, "uk": Uk, "be": Be, "pl": Pl, "cs": Cs, "sk": Sk, "sl": Sl,
	"hr": Hr, "sr": Sr, "bg": Bg, "mk": Mk,
	"lv": Lv, "lt": Lt, "fi": Fi, "et": Et, "hu": Hu, "el": El, "sq": Sq,
	"tr": Tr, "eu": Eu,
	"ja": Ja, "zh": Zh, "ko": Ko, "th": Th, "vi": Vi, "id": Id, "ms": Ms,
	"hi": Hi, "ar": Ar, "he": He,
}

// All returns every rule keyed by base language. The returned map is a copy;
// mutating it affects nothing.
func All() map[string]i18n.PluralRule {
	out := make(map[string]i18n.PluralRule, len(all))
	maps.Copy(out, all)
	return out
}

// PluralFor looks up a rule by language tag, reducing a regional tag to its
// base language ("pt-BR" → "pt"). ok is false for a language this package does
// not carry — the caller then leaves it unwired, and core/i18n's DefaultRule
// applies, which is honest about knowing nothing rather than guessing.
//
// This is a convenience for callers. core/i18n never calls it.
func PluralFor(lang string) (i18n.PluralRule, bool) {
	if lang == "" {
		return nil, false
	}
	base, _, _ := strings.Cut(strings.ToLower(strings.ReplaceAll(lang, "_", "-")), "-")
	r, ok := all[base]
	return r, ok
}
