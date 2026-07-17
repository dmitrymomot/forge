package cldr_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	xplural "golang.org/x/text/feature/plural"
	"golang.org/x/text/language"

	"github.com/dmitrymomot/forge/core/i18n"
	"github.com/dmitrymomot/forge/core/i18n/cldr"
)

// xForm maps x/text's Form to our PluralCategory.
func xForm(f xplural.Form) (i18n.PluralCategory, bool) {
	switch f {
	case xplural.Zero:
		return i18n.Zero, true
	case xplural.One:
		return i18n.One, true
	case xplural.Two:
		return i18n.Two, true
	case xplural.Few:
		return i18n.Few, true
	case xplural.Many:
		return i18n.Many, true
	case xplural.Other:
		return i18n.Other, true
	}
	return 0, false
}

// staleExceptions lists (lang, n) pairs where x/text's CLDR vintage disagrees
// with modern CLDR and OUR rule is the correct one. Every entry needs a reason.
//
// CLDR 38 (2020) added the whole-millions "many" category to the Romance
// languages; x/text v0.38 predates it and still reports "other".
var staleExceptions = map[string]map[int]string{
	"fr": {1000000: "CLDR 38 Romance whole-millions many", 2000000: "ditto", 3000000: "ditto", 10000000: "ditto"},
	"es": {1000000: "CLDR 38 Romance whole-millions many", 2000000: "ditto", 3000000: "ditto", 10000000: "ditto"},
	"it": {1000000: "CLDR 38 Romance whole-millions many", 2000000: "ditto", 3000000: "ditto", 10000000: "ditto"},
	"pt": {1000000: "CLDR 38 Romance whole-millions many", 2000000: "ditto", 3000000: "ditto", 10000000: "ditto"},
	"ca": {1000000: "CLDR 38 Romance whole-millions many", 2000000: "ditto", 3000000: "ditto", 10000000: "ditto"},
	// x/text v0.38 omits CLDR's i%100!=11 exception for Macedonian, so it
	// wrongly says "one" at every n%10==1 && n%100==11 probe value.
	"mk": {
		11:   "x/text v0.38 Macedonian bug: omits CLDR i%100!=11 exception",
		111:  "x/text v0.38 Macedonian bug: omits CLDR i%100!=11 exception",
		211:  "x/text v0.38 Macedonian bug: omits CLDR i%100!=11 exception",
		1011: "x/text v0.38 Macedonian bug: omits CLDR i%100!=11 exception",
	},
	// x/text v0.38 predates CLDR v42, which removed Hebrew's round-tens
	// "many" category; x/text still reports "many" for those values.
	"he": {
		20:       "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		30:       "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		40:       "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		50:       "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		60:       "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		70:       "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		80:       "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		90:       "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		100:      "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		110:      "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		120:      "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		130:      "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		200:      "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		300:      "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		1000:     "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		10000:    "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		100000:   "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		1000000:  "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		2000000:  "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		3000000:  "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
		10000000: "x/text v0.38 stale: CLDR removed Hebrew round-tens 'many' at v42",
	},
}

// probe covers every band real CLDR rules discriminate on.
func probe() []int {
	ns := make([]int, 0, 256)
	for n := 0; n <= 130; n++ {
		ns = append(ns, n)
	}
	ns = append(ns,
		200, 201, 202, 203, 211, 300, 999, 1000, 1001, 1002, 1011,
		10000, 100000, 1000000, 1000001, 2000000, 3000000, 10000000,
	)
	return ns
}

// TestRulesMatchCLDR is the acceptance criterion for this whole package: every
// rule is differential-tested against x/text's CLDR-generated tables.
func TestRulesMatchCLDR(t *testing.T) {
	t.Parallel()
	for lang, rule := range cldr.All() {
		t.Run(lang, func(t *testing.T) {
			t.Parallel()
			tag := language.MustParse(lang)
			for _, n := range probe() {
				want, ok := xForm(xplural.Cardinal.MatchPlural(tag, n, 0, 0, 0, 0))
				require.Truef(t, ok, "x/text returned no form for %s(%d)", lang, n)
				got := rule(n)
				if got == want {
					continue
				}
				if reason, excepted := staleExceptions[lang][n]; excepted {
					t.Logf("known x/text staleness at %s(%d): ours=%v x/text=%v (%s)", lang, n, got, want, reason)
					continue
				}
				assert.Failf(t, "CLDR mismatch",
					"%s(%d) = %v, x/text says %v — verify against the CLDR spec; if x/text is stale, add it to staleExceptions with a reason",
					lang, n, got, want)
			}
		})
	}
}

// TestRulesAreTotal pins the never-panics constraint at the integer extremes.
func TestRulesAreTotal(t *testing.T) {
	t.Parallel()
	for lang, rule := range cldr.All() {
		for _, n := range []int{math.MinInt, math.MinInt + 1, -1000000, -21, -1, 0, math.MaxInt} {
			assert.NotPanicsf(t, func() {
				got := rule(n)
				assert.Lessf(t, int(got), 6, "%s(%d) returned an out-of-range category", lang, n)
			}, "%s(%d) panicked", lang, n)
		}
	}
}

// TestNegativesMirrorPositives: CLDR rules are defined on the absolute value.
func TestNegativesMirrorPositives(t *testing.T) {
	t.Parallel()
	for lang, rule := range cldr.All() {
		for _, n := range []int{1, 2, 5, 11, 21, 101, 1000000} {
			assert.Equalf(t, rule(n), rule(-n), "%s: rule(%d) != rule(%d)", lang, n, -n)
		}
	}
}

// TestKnownCases pins the two historical bugs this package exists to prevent,
// independently of x/text.
func TestKnownCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		lang string
		n    int
		want i18n.PluralCategory
	}{
		// The Slavic family-bucket bug: uk/ru 21 is one, not many.
		{"uk", 21, i18n.One}, {"ru", 21, i18n.One},
		{"uk", 11, i18n.Many}, {"ru", 11, i18n.Many},
		{"uk", 0, i18n.Many}, {"ru", 0, i18n.Many},
		{"uk", 2, i18n.Few}, {"ru", 22, i18n.Few},
		// Polish differs from East Slavic at 21.
		{"pl", 21, i18n.Many}, {"pl", 1, i18n.One}, {"pl", 2, i18n.Few},
		// The Romance family-bucket bug: pt 0 is one, it 0 is other.
		{"pt", 0, i18n.One}, {"it", 0, i18n.Other},
		{"fr", 0, i18n.One}, {"es", 0, i18n.Other},
		// Arabic's six bands.
		{"ar", 0, i18n.Zero}, {"ar", 1, i18n.One}, {"ar", 2, i18n.Two},
		{"ar", 3, i18n.Few}, {"ar", 11, i18n.Many}, {"ar", 100, i18n.Other},
		// No plural distinction.
		{"ja", 1, i18n.Other}, {"zh", 5, i18n.Other}, {"vi", 1, i18n.Other},
		// Macedonian: CLDR's i%100!=11 exception (x/text v0.38 misses this).
		{"mk", 1, i18n.One}, {"mk", 11, i18n.Other}, {"mk", 21, i18n.One}, {"mk", 111, i18n.Other},
		// Hebrew: no round-tens "many" in modern CLDR (x/text v0.38 is stale here).
		{"he", 1, i18n.One}, {"he", 2, i18n.Two}, {"he", 3, i18n.Other}, {"he", 20, i18n.Other}, {"he", 100, i18n.Other},
	}
	for _, c := range cases {
		rule, ok := cldr.PluralFor(c.lang)
		require.Truef(t, ok, "no rule for %s", c.lang)
		// %v renders the category name; testify's raw output would print hex.
		got := rule(c.n)
		assert.Equalf(t, c.want, got, "%s(%d) = %v, want %v", c.lang, c.n, got, c.want)
	}
}

func TestPluralFor(t *testing.T) {
	t.Parallel()
	r, ok := cldr.PluralFor("uk")
	require.True(t, ok)
	assert.Equal(t, i18n.One, r(21))

	// Regional tags reduce to their language.
	r, ok = cldr.PluralFor("pt-BR")
	require.True(t, ok)
	assert.Equal(t, i18n.One, r(0))

	// Unknown languages report false rather than guessing — the caller then
	// gets i18n's DefaultRule, which is honest about knowing nothing.
	_, ok = cldr.PluralFor("xx")
	assert.False(t, ok)
	_, ok = cldr.PluralFor("")
	assert.False(t, ok)
	// Deliberately excluded from scope.
	for _, lang := range []string{"ga", "cy", "mt"} {
		_, ok := cldr.PluralFor(lang)
		assert.Falsef(t, ok, "%s is out of scope and must not be present", lang)
	}
}

func TestAllIsComplete(t *testing.T) {
	t.Parallel()
	want := []string{
		"en", "de", "nl", "sv", "da", "nb", "is",
		"fr", "es", "it", "pt", "ro", "ca", "gl",
		"ru", "uk", "be", "pl", "cs", "sk", "sl", "hr", "sr", "bg", "mk",
		"lv", "lt", "fi", "et", "hu", "el", "sq", "tr", "eu",
		"ja", "zh", "ko", "th", "vi", "id", "ms", "hi", "ar", "he",
	}
	all := cldr.All()
	for _, lang := range want {
		_, ok := all[lang]
		assert.Truef(t, ok, "missing rule for %s", lang)
	}
	assert.Len(t, all, len(want))
	for lang, rule := range all {
		require.NotNilf(t, rule, "nil rule for %s", lang)
		_ = fmt.Sprint(lang)
	}
}
