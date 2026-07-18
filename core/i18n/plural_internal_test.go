package i18n

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormFallback(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from PluralCategory
		want []PluralCategory
	}{
		{Zero, []PluralCategory{Other}},
		{One, []PluralCategory{Other}},
		{Two, []PluralCategory{Few, Many, Other}},
		{Few, []PluralCategory{Many, Other}},
		{Many, []PluralCategory{Other}},
		{Other, nil},
	}
	for _, c := range cases {
		got := formFallback(c.from)
		assert.Truef(t, slices.Equal(got, c.want), "formFallback(%v) = %v, want %v", c.from, got, c.want)
	}
}

func TestPluralCategoryString(t *testing.T) {
	t.Parallel()
	want := map[PluralCategory]string{Zero: "zero", One: "one", Two: "two", Few: "few", Many: "many", Other: "other"}
	for c, s := range want {
		assert.Equalf(t, s, c.String(), "PluralCategory(%d).String()", uint8(c))
	}
	// Out-of-range degrades to the terminal form rather than panicking.
	assert.Equal(t, "other", PluralCategory(200).String())
}

func TestCategoryByName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]PluralCategory{
		"zero": Zero, "one": One, "two": Two, "few": Few, "many": Many, "other": Other,
	} {
		got, ok := categoryByName(name)
		require.Truef(t, ok, "categoryByName(%q) not ok", name)
		assert.Equalf(t, want, got, "categoryByName(%q)", name)
	}
	for _, name := range []string{"", "save", "One", "ZERO", "otherx"} {
		_, ok := categoryByName(name)
		assert.Falsef(t, ok, "categoryByName(%q) should not resolve", name)
	}
}

func TestDefaultRule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int
		want PluralCategory
	}{
		{0, Zero},
		{1, One},
		{-1, One},
		{2, Many},
		{5, Many},
		{-7, Many},
		{100, Many},
		{math.MaxInt, Many},
		{math.MinInt, Many}, // total: no abs() overflow path
	}
	for _, c := range cases {
		// Name the categories in the message: PluralCategory is uint8-based, so
		// testify's own expected/actual output renders it as hex (0x4), which is
		// unreadable. String() exists for exactly this.
		got := DefaultRule(c.n)
		assert.Equalf(t, c.want, got, "DefaultRule(%d) = %v, want %v", c.n, got, c.want)
	}
}

func TestSupportedForms(t *testing.T) {
	t.Parallel()
	oneOther := PluralRule(func(n int) PluralCategory {
		if n == 1 || n == -1 {
			return One
		}
		return Other
	})
	got := SupportedForms(oneOther)
	assert.Truef(t, slices.Equal(got, []PluralCategory{One, Other}), "SupportedForms(oneOther) = %v", got)

	// The built-in default produces exactly zero/one/many.
	got = SupportedForms(DefaultRule)
	assert.Truef(t, slices.Equal(got, []PluralCategory{Zero, One, Many}), "SupportedForms(DefaultRule) = %v", got)

	// Results are in canonical category order regardless of probe order.
	always := PluralRule(func(int) PluralCategory { return Other })
	assert.Truef(t, slices.Equal(SupportedForms(always), []PluralCategory{Other}), "SupportedForms(always-other)")
}
