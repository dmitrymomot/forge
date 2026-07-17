package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		assert.Equal(t, c.want, formFallback(c.from))
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
	assert.Equal(t, []PluralCategory{One, Other}, SupportedForms(oneOther))
}

func TestPluralCategoryString(t *testing.T) {
	t.Parallel()
	want := map[PluralCategory]string{Zero: "zero", One: "one", Two: "two", Few: "few", Many: "many", Other: "other"}
	for c, s := range want {
		assert.Equal(t, s, c.String())
	}
}
