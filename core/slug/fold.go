package slug

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// specialFold covers Latin letters that NFKD does NOT decompose to ASCII + a
// combining mark, so the norm-based path cannot reach them. Keys are lowercase;
// Make lowercases runes before folding, so only lowercase keys are needed.
var specialFold = map[rune]string{
	'ß': "ss",
	'ø': "o",
	'ł': "l",
	'đ': "d",
	'æ': "ae",
	'œ': "oe",
}

// foldRune folds a single rune to its ASCII slug contribution. The second return
// value is true when the rune produced at least one [a-z0-9] character; false
// means the rune contributes no sluggable output (and the caller emits a
// separator instead). The rune is assumed already lowercased by the caller when
// cfg.lowercase is set.
func foldRune(r rune) (string, bool) {
	// ASCII alphanumerics pass through untouched.
	if isASCIIAlphaNum(r) {
		return string(r), true
	}
	// Non-decomposing Latin special cases.
	if repl, ok := specialFold[r]; ok {
		return repl, true
	}
	// NFKD decomposition: drop combining marks and keep any resulting ASCII
	// alphanumerics (é ⇒ "e", ǆ ⇒ "dz", ﬁ ⇒ "fi", ½ ⇒ "12").
	decomposed := norm.NFKD.String(string(r))
	var b strings.Builder
	for _, d := range decomposed {
		if unicode.Is(unicode.Mn, d) { // combining mark
			continue
		}
		if isASCIIAlphaNum(d) {
			b.WriteRune(d)
		}
	}
	if b.Len() > 0 {
		return b.String(), true
	}
	return "", false
}
