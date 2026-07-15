package country_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/country"
)

func flag(alpha2 string) string {
	const base = 0x1F1E6
	return string([]rune{rune(base + int(alpha2[0]-'A')), rune(base + int(alpha2[1]-'A'))})
}

func TestTable_Invariants(t *testing.T) {
	all := country.All()
	assert.GreaterOrEqual(t, len(all), 240, "expected the full ISO-3166-1 set")

	seenA2 := map[string]bool{}
	seenA3 := map[string]bool{}
	seenNum := map[string]bool{}
	for _, c := range all {
		assert.Len(t, c.Alpha2, 2, "alpha2 %q", c.Alpha2)
		assert.Len(t, c.Alpha3, 3, "alpha3 %q", c.Alpha3)
		assert.Len(t, c.Numeric, 3, "numeric %q for %s", c.Numeric, c.Alpha2)
		assert.NotEmpty(t, c.Name, "name for %s", c.Alpha2)
		assert.Len(t, c.Currency, 3, "currency %q for %s", c.Currency, c.Alpha2)
		assert.NotEmpty(t, c.DialCode, "dial for %s", c.Alpha2)
		for i := range len(c.DialCode) {
			assert.True(t, c.DialCode[i] >= '0' && c.DialCode[i] <= '9', "dial %q not numeric", c.DialCode)
		}
		assert.LessOrEqual(t, len(c.DialCode), 3, "dial %q too long", c.DialCode)
		assert.Equal(t, flag(c.Alpha2), c.Emoji, "emoji mismatch for %s", c.Alpha2)

		assert.False(t, seenA2[c.Alpha2], "duplicate alpha2 %s", c.Alpha2)
		assert.False(t, seenA3[c.Alpha3], "duplicate alpha3 %s", c.Alpha3)
		assert.False(t, seenNum[c.Numeric], "duplicate numeric %s", c.Numeric)
		seenA2[c.Alpha2], seenA3[c.Alpha3], seenNum[c.Numeric] = true, true, true
	}
}

func TestTable_KnownSpotChecks(t *testing.T) {
	for _, tc := range []struct{ code, name, cur, dial string }{
		{"AD", "Andorra", "EUR", "376"},
		{"CN", "China", "CNY", "86"},
		{"EG", "Egypt", "EGP", "20"},
		{"NZ", "New Zealand", "NZD", "64"},
		{"ZW", "Zimbabwe", "ZWG", "263"},
	} {
		c, ok := country.ByAlpha2(tc.code)
		assert.True(t, ok, tc.code)
		assert.Equal(t, tc.name, c.Name, tc.code)
		assert.Equal(t, tc.cur, c.Currency, tc.code)
		assert.Equal(t, tc.dial, c.DialCode, tc.code)
	}
}
