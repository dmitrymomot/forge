package i18n

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAcceptLanguage(t *testing.T) {
	t.Parallel()
	got := parseAcceptLanguage("es-ES,es;q=0.9,en;q=0.8")
	require.Len(t, got, 3)
	assert.Equal(t, "es-ES", got[0].tag)
	assert.InDelta(t, 1.0, got[0].q, 1e-9)
	assert.Equal(t, "es", got[1].tag)
	assert.InDelta(t, 0.9, got[1].q, 1e-9)
	assert.Equal(t, "en", got[2].tag)
	assert.InDelta(t, 0.8, got[2].q, 1e-9)
}

func TestParseAcceptLanguageSorting(t *testing.T) {
	t.Parallel()
	got := parseAcceptLanguage("en;q=0.5,de;q=0.9,fr")
	require.Len(t, got, 3)
	assert.Equal(t, "fr", got[0].tag, "implicit q=1 sorts first")
	assert.Equal(t, "de", got[1].tag)
	assert.Equal(t, "en", got[2].tag)
}

func TestParseAcceptLanguageTieBreakKeepsHeaderOrder(t *testing.T) {
	t.Parallel()
	// Equal q must preserve header order so server preference can break the
	// tie deterministically (RFC 7231 5.3.1).
	got := parseAcceptLanguage("de;q=0.8,fr;q=0.8,es;q=0.8")
	require.Len(t, got, 3)
	assert.Equal(t, []string{"de", "fr", "es"}, []string{got[0].tag, got[1].tag, got[2].tag})
}

func TestParseAcceptLanguageRejects(t *testing.T) {
	t.Parallel()
	// q=0 means "not acceptable".
	assert.Empty(t, parseAcceptLanguage("en;q=0"))
	// Out-of-range, unparseable, and NaN q-values drop the tag.
	assert.Empty(t, parseAcceptLanguage("en;q=1.5"))
	assert.Empty(t, parseAcceptLanguage("en;q=-1"))
	assert.Empty(t, parseAcceptLanguage("en;q=abc"))
	assert.Empty(t, parseAcceptLanguage("en;q=NaN"))
	assert.Empty(t, parseAcceptLanguage("en;q="))
	// Junk.
	assert.Empty(t, parseAcceptLanguage(""))
	assert.Empty(t, parseAcceptLanguage(",,,"))
	assert.Empty(t, parseAcceptLanguage(";q=0.5"))
}

func TestParseAcceptLanguageCaseInsensitiveQ(t *testing.T) {
	t.Parallel()
	// "Q=" (uppercase) must be recognized the same as "q=".
	got := parseAcceptLanguage("en;Q=0.5")
	require.Len(t, got, 1)
	assert.Equal(t, "en", got[0].tag)
	assert.InDelta(t, 0.5, got[0].q, 1e-9)
}

func TestParseAcceptLanguageWhitespace(t *testing.T) {
	t.Parallel()
	// Extra whitespace around ranges and the q parameter must not break
	// parsing.
	got := parseAcceptLanguage(" en ; q=0.5 , fr ")
	require.Len(t, got, 2)
	assert.Equal(t, "fr", got[0].tag)
	assert.Equal(t, "en", got[1].tag)
}

func TestParseAcceptLanguageWildcard(t *testing.T) {
	t.Parallel()
	got := parseAcceptLanguage("*")
	require.Len(t, got, 1)
	assert.Equal(t, "*", got[0].tag)
}

func TestParseAcceptLanguageCaps(t *testing.T) {
	t.Parallel()
	// Oversized headers are rejected outright, before any parsing work.
	assert.Empty(t, parseAcceptLanguage(strings.Repeat("en,", maxAcceptLangLen)))

	// A header within the size cap but with too many tags is truncated, not
	// rejected: the client still gets its highest-priority choices.
	var sb strings.Builder
	for i := range maxAcceptLangTags + 20 {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("en")
	}
	require.Less(t, sb.Len(), maxAcceptLangLen)
	assert.LessOrEqual(t, len(parseAcceptLanguage(sb.String())), maxAcceptLangTags)
}

func TestParseAcceptLanguageIsTotal(t *testing.T) {
	t.Parallel()
	// Must never panic on adversarial input.
	for _, in := range []string{"\x00", "\xff\xfe", ";;;;;", "=====", "q=q=q=q", strings.Repeat(";", 200), "en;q=0.999999999999999999999999999"} {
		assert.NotPanicsf(t, func() { _ = parseAcceptLanguage(in) }, "parseAcceptLanguage(%q)", in)
	}
}

func FuzzParseAcceptLanguage(f *testing.F) {
	f.Add("es-ES,es;q=0.9,en;q=0.8")
	f.Add("*")
	f.Add(";q=")
	f.Add("en;q=NaN")
	f.Fuzz(func(t *testing.T, s string) {
		got := parseAcceptLanguage(s)
		assert.LessOrEqual(t, len(got), maxAcceptLangTags)
		for _, lq := range got {
			// Every surviving entry must have a sane q and a normalized tag.
			assert.GreaterOrEqual(t, lq.q, 0.0)
			assert.LessOrEqual(t, lq.q, 1.0)
			assert.Equal(t, lq.q, lq.q, "NaN must never survive parsing")
		}
	})
}
