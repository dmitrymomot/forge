package i18n

import "strings"

// Locale is a normalized language tag. It is comparable, pointer-free, and
// globally meaningful: the same tag means the same thing in every Bundle.
//
// Any normalized tag is a valid Locale — this package ships no list of
// languages and validates against none. Whether a Locale is *supported* is a
// question only a Bundle can answer, from the catalogs it loaded.
//
// The zero Locale is invalid and reads as "unresolved": every consumer falls
// back to the bundle default.
type Locale struct {
	tag string
}

// newLocale normalizes tag into a Locale. Input that cannot be normalized
// yields the zero Locale.
func newLocale(tag string) Locale {
	return Locale{tag: normalizeTag(tag)}
}

// IsZero reports whether the Locale is the invalid zero value.
func (l Locale) IsZero() bool { return l.tag == "" }

// Tag returns the canonical tag ("uk", "pt-BR"); "" for the zero Locale.
func (l Locale) Tag() string { return l.tag }

// Lang returns the base language ("pt" for "pt-BR"); "" for the zero Locale.
func (l Locale) Lang() string {
	if base, _, ok := strings.Cut(l.tag, "-"); ok {
		return base
	}
	return l.tag
}

// String implements fmt.Stringer; identical to Tag.
func (l Locale) String() string { return l.tag }

// maxTagLen bounds attacker-controlled tags (cookies, query params,
// Accept-Language) before any normalization work happens.
const maxTagLen = 35

// normalizeTag canonicalizes a language tag: trims space, maps '_' to '-',
// keeps at most the first two subtags, lowercases the language and uppercases
// the region. Returns "" for empty, oversized, or structurally invalid input
// (an absent language subtag). It is total: every input either normalizes or
// yields "".
//
// Note the two-subtag rule reads a script subtag as a region ("zh-Hans-CN" →
// "zh-HANS"). That is intentional and harmless: the tag simply will not match
// a catalog, and Bundle's base-language fallback resolves it to "zh".
//
// Case folding is ASCII-only rather than strings.ToUpper/ToLower: BCP-47
// subtags are ASCII, and Unicode-aware folding can expand a rune's byte
// length (most sharply for invalid UTF-8, where each stray byte decodes to
// utf8.RuneError and re-encodes three bytes wide). ASCII folding never
// changes byte length, which is what keeps normalizeTag idempotent — no
// under-the-limit tag can grow past maxTagLen on a second pass.
//
// Each subtag is also trimmed individually after Cut, not just the input as
// a whole: a hyphen (or an underscore that became one) can leave whitespace
// stranded at a subtag boundary that the leading TrimSpace never sees (e.g.
// "a _" → lang "a "). Left untrimmed, that whitespace would ride into the
// result and then vanish on a second pass, which would break idempotency.
func normalizeTag(s string) string {
	// Bound before any work: an attacker-controlled string (cookie, header)
	// could otherwise force a full TrimSpace scan before we ever reject it.
	if len(s) > maxTagLen {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "_", "-")
	lang, rest, hasRegion := strings.Cut(s, "-")
	if lang == "" {
		return "" // "-US", "-", "--": no language subtag
	}
	lang = asciiLower(strings.TrimSpace(lang))
	if !hasRegion {
		return lang
	}
	region, _, _ := strings.Cut(rest, "-")
	region = asciiUpper(strings.TrimSpace(region))
	if region == "" {
		return lang // "en-", or a region that was pure whitespace
	}
	return lang + "-" + region
}

// asciiLower lowercases only ASCII 'A'-'Z' bytes, leaving every other byte
// (including invalid UTF-8) untouched. Unlike strings.ToLower, it never
// changes the byte length of s.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// asciiUpper uppercases only ASCII 'a'-'z' bytes; see asciiLower.
func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}
