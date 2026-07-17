package i18n_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNegotiate(t *testing.T) {
	t.Parallel()
	b := newBundle(t) // en, en-GB, uk, de, vi; default en

	cases := map[string]string{
		"uk":                      "uk",
		"uk-UA":                   "uk",    // region falls back to base
		"en-GB":                   "en-GB", // exact regional match wins
		"de-AT,de;q=0.9":          "de",    // de-AT unsupported, de is
		"fr-FR,uk;q=0.9,en;q=0.8": "uk",    // fr unsupported, uk is next
		"fr,es":                   "en",    // nothing supported: default
		"*":                       "en",    // wildcard: default
		"":                        "en",    // absent header: default
		"vi":                      "vi",    // a language the package knows nothing about
		"zh-Hans-CN":              "en",    // unsupported: default
		"en;q=0.1,uk;q=0.9":       "uk",    // q ordering respected
		"uk;q=0,en":               "en",    // q=0 rejects uk
	}
	for header, want := range cases {
		assert.Equalf(t, want, b.Negotiate(header).Tag(), "Negotiate(%q)", header)
	}
}

func TestNegotiateServerPreferenceTieBreak(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	// Equal q: the first supported tag in header order wins.
	assert.Equal(t, "uk", b.Negotiate("uk;q=0.8,de;q=0.8").Tag())
	assert.Equal(t, "de", b.Negotiate("de;q=0.8,uk;q=0.8").Tag())
}

func TestNegotiateNeverReturnsZeroLocale(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	for _, header := range []string{"", "*", "fr,es", "garbage;q=abc", strings.Repeat("x", 5000)} {
		assert.Falsef(t, b.Negotiate(header).IsZero(), "Negotiate(%q)", header)
	}
}

func TestNegotiateDoS(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	// A megabyte header must be rejected by the size cap, not sorted.
	assert.Equal(t, "en", b.Negotiate(strings.Repeat("uk,", 500_000)).Tag())
	// A header inside the size cap but with hundreds of tags still terminates
	// and still resolves the first supported tag.
	assert.Equal(t, "uk", b.Negotiate("uk,"+strings.Repeat("xx,", 100)).Tag())
}
