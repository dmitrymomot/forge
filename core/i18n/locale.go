package i18n

import "strings"

// Locale is an interned reference into the curated locale table. The zero
// Locale is invalid; every consumer treats it as "unresolved" and falls back
// to the bundle default. Locale is comparable and pointer-free.
type Locale struct {
	idx uint16 // 1-based table index; 0 = invalid
}

// IsZero reports whether the Locale is the invalid zero value.
func (l Locale) IsZero() bool { return l.idx == 0 }

// info returns the table row, or nil for the zero Locale.
func (l Locale) info() *localeInfo {
	if l.idx == 0 || int(l.idx) > len(localeTable) {
		return nil
	}
	return &localeTable[l.idx-1]
}

// Tag returns the canonical BCP-47-style tag ("uk", "pt-BR"); "" for zero.
func (l Locale) Tag() string {
	if li := l.info(); li != nil {
		return li.tag
	}
	return ""
}

// Lang returns the base language ("pt" for "pt-BR"); "" for zero.
func (l Locale) Lang() string {
	if li := l.info(); li != nil {
		return li.lang
	}
	return ""
}

// String implements fmt.Stringer; identical to Tag.
func (l Locale) String() string { return l.Tag() }

// maxTagLen bounds attacker-controlled tags (cookies, query params) before
// normalization work.
const maxTagLen = 35

// normalizeTag canonicalizes a language tag: trims space, maps '_' to '-',
// keeps at most the first two subtags, lowercases the language and uppercases
// the region. Returns "" for empty or oversized input.
func normalizeTag(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxTagLen {
		return ""
	}
	s = strings.ReplaceAll(s, "_", "-")
	lang, rest, hasRegion := strings.Cut(s, "-")
	if !hasRegion {
		return strings.ToLower(lang)
	}
	region, _, _ := strings.Cut(rest, "-")
	if region == "" {
		return strings.ToLower(lang)
	}
	return strings.ToLower(lang) + "-" + strings.ToUpper(region)
}

// localeByTag is built once at init from localeTable.
var localeByTag = func() map[string]Locale {
	m := make(map[string]Locale, len(localeTable))
	for i := range localeTable {
		m[localeTable[i].tag] = Locale{idx: uint16(i + 1)}
	}
	return m
}()

// lookupLocale interns a raw tag: exact canonical match first, then the base
// language ("en-AU" → "en"). ok=false when neither is curated.
func lookupLocale(tag string) (Locale, bool) {
	norm := normalizeTag(tag)
	if norm == "" {
		return Locale{}, false
	}
	if l, ok := localeByTag[norm]; ok {
		return l, true
	}
	if lang, _, ok := strings.Cut(norm, "-"); ok {
		if l, ok := localeByTag[lang]; ok {
			return l, true
		}
	}
	return Locale{}, false
}
