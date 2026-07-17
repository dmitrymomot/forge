package i18n

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeTag(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"en":                             "en",
		"EN":                             "en",
		"en-us":                          "en-US",
		"en_US":                          "en-US",
		"UK-ua":                          "uk-UA",
		"pt-br":                          "pt-BR",
		"zh-cn":                          "zh-CN",
		" en-GB ":                        "en-GB",
		"fr-CA-x-ca":                     "fr-CA", // only the first two subtags are kept
		"vi":                             "vi",    // a tag this package knows nothing about is still valid
		"ww-WW":                          "ww-WW", // ditto: no allowlist exists
		"":                               "",
		"-":                              "",
		"--":                             "",
		"-US":                            "", // empty language is not a tag
		"en-":                            "en",
		"_":                              "",
		strings.Repeat("a", maxTagLen+1): "", // oversized input is rejected before any work
	}
	for in, want := range cases {
		assert.Equalf(t, want, normalizeTag(in), "normalizeTag(%q)", in)
	}
}

func TestNormalizeTagIsTotal(t *testing.T) {
	t.Parallel()
	// Must never panic on adversarial input.
	for _, in := range []string{"\x00", "\xff\xfe", "en-\x00US", "---", "a-b-c-d-e", "\t\n", "EN_us_POSIX"} {
		assert.NotPanicsf(t, func() { _ = normalizeTag(in) }, "normalizeTag(%q)", in)
	}
}

func TestLocale(t *testing.T) {
	t.Parallel()
	l := newLocale("pt-br")
	assert.Equal(t, "pt-BR", l.Tag())
	assert.Equal(t, "pt", l.Lang())
	assert.Equal(t, "pt-BR", l.String())
	assert.False(t, l.IsZero())

	// A tag with no region: Lang == Tag.
	l = newLocale("uk")
	assert.Equal(t, "uk", l.Tag())
	assert.Equal(t, "uk", l.Lang())

	// Unnormalizable input yields the zero Locale.
	l = newLocale("-")
	assert.True(t, l.IsZero())

	// The zero Locale is safe and empty.
	var zero Locale
	assert.True(t, zero.IsZero())
	assert.Empty(t, zero.Tag())
	assert.Empty(t, zero.Lang())
	assert.Empty(t, zero.String())
}

func TestLocaleComparable(t *testing.T) {
	t.Parallel()
	// Normalization makes equality meaningful across input spellings.
	assert.Equal(t, newLocale("en_US"), newLocale("EN-us"))
	assert.NotEqual(t, newLocale("en"), newLocale("en-GB"))
}

func FuzzNormalizeTag(f *testing.F) {
	f.Add("en-US")
	f.Add("pt_br")
	f.Add("---")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		got := normalizeTag(s)
		// Normalization is idempotent: re-normalizing a normalized tag is a no-op.
		if got != "" {
			assert.Equal(t, got, normalizeTag(got))
		}
	})
}
