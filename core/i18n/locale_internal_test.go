package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTag(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"en":         "en",
		"EN":         "en",
		"en-us":      "en-US",
		"en_US":      "en-US",
		"UK-ua":      "uk-UA",
		"pt-br":      "pt-BR",
		"zh-cn":      "zh-CN",
		" en-GB ":    "en-GB",
		"fr-CA-x-ca": "fr-CA", // only first two subtags kept
		"":           "",
	}
	for in, want := range cases {
		assert.Equalf(t, want, normalizeTag(in), "normalizeTag(%q)", in)
	}
}

func TestLookupLocale(t *testing.T) {
	t.Parallel()
	// Exact tag match.
	loc, ok := lookupLocale("pt-BR")
	require.True(t, ok)
	assert.Equal(t, "pt-BR", loc.Tag())
	assert.Equal(t, "pt", loc.Lang())
	// Region falls back to base language entry: en-AU → en.
	loc, ok = lookupLocale("en-AU")
	require.True(t, ok)
	assert.Equal(t, "en", loc.Tag())
	// Curated regional variant is distinct from base.
	gb, _ := lookupLocale("en-GB")
	en, _ := lookupLocale("en")
	assert.NotEqual(t, en, gb, "en-GB and en must intern to different locales")
	// Unknown language fails.
	_, ok = lookupLocale("xx")
	assert.False(t, ok, "xx should not resolve")
	// Zero Locale is invalid and safe.
	var zero Locale
	assert.True(t, zero.IsZero())
	assert.Equal(t, "", zero.Tag())
	assert.Equal(t, "", zero.Lang())
}

func TestLocaleTableIntegrity(t *testing.T) {
	t.Parallel()
	require.Len(t, localeTable, 16, "expected 16 curated locales")
	seen := map[string]bool{}
	for i := range localeTable {
		li := &localeTable[i]
		assert.Falsef(t, seen[li.tag], "duplicate tag %q", li.tag)
		seen[li.tag] = true
		assert.NotNilf(t, li.rule, "%s: nil plural rule", li.tag)
		f := &li.format
		assert.NotEmptyf(t, f.DecimalSep, "%s: incomplete FormatSpec (DecimalSep)", li.tag)
		assert.NotEmptyf(t, f.GroupSep, "%s: incomplete FormatSpec (GroupSep)", li.tag)
		assert.NotEmptyf(t, f.DateLayout, "%s: incomplete FormatSpec (DateLayout)", li.tag)
		assert.NotEmptyf(t, f.TimeLayout, "%s: incomplete FormatSpec (TimeLayout)", li.tag)
		assert.NotEmptyf(t, f.DateTimeLayout, "%s: incomplete FormatSpec (DateTimeLayout)", li.tag)
		r := &f.Relative
		assert.NotEmptyf(t, r.Now, "%s: incomplete RelativeSpec templates (Now)", li.tag)
		assert.NotEmptyf(t, r.Past, "%s: incomplete RelativeSpec templates (Past)", li.tag)
		assert.NotEmptyf(t, r.Future, "%s: incomplete RelativeSpec templates (Future)", li.tag)
		for u := range numRelUnits {
			assert.NotEmptyf(t, r.Units[u][Other], "%s: relative unit %d missing Other form", li.tag, u)
		}
		if r.FutureUnits != nil {
			for u := range numRelUnits {
				assert.NotEmptyf(t, r.FutureUnits[u][Other], "%s: future unit %d missing Other form", li.tag, u)
			}
		}
	}
	for _, tag := range []string{"en", "en-GB", "de", "fr", "es", "it", "pt-BR", "pl", "cs", "uk", "ru", "nl", "tr", "ar", "ja", "zh-CN"} {
		assert.Truef(t, seen[tag], "missing curated locale %s", tag)
	}
}
