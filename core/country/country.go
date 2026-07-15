package country

import (
	"sort"
	"strings"
)

// Country is one ISO-3166-1 entry: its alpha-2 (the canonical key), alpha-3, and
// numeric-3 codes, English short name, primary official ISO-4217 currency code,
// E.164 dial code (no leading +), and flag emoji.
type Country struct {
	Alpha2   string
	Alpha3   string
	Numeric  string
	Name     string
	Currency string
	DialCode string
	Emoji    string
}

var (
	byAlpha2  = map[string]Country{}
	byAlpha3  = map[string]Country{}
	byNumeric = map[string]Country{}
	byDial    = map[string][]Country{}
	sorted    []Country
)

func init() {
	for _, c := range all {
		c.Emoji = flagEmoji(c.Alpha2)
	}
	sorted = make([]Country, 0, len(all))
	for _, c := range all {
		byAlpha2[c.Alpha2] = *c
		byAlpha3[c.Alpha3] = *c
		byNumeric[c.Numeric] = *c
		byDial[c.DialCode] = append(byDial[c.DialCode], *c)
		sorted = append(sorted, *c)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for k := range byDial {
		s := byDial[k]
		sort.Slice(s, func(i, j int) bool { return s[i].Name < s[j].Name })
	}
}

// flagEmoji derives a flag from an alpha-2 code by mapping each letter to its
// Unicode regional-indicator symbol. It returns "" for non-two-letter or
// non-A–Z input.
func flagEmoji(alpha2 string) string {
	if len(alpha2) != 2 {
		return ""
	}
	a, b := alpha2[0], alpha2[1]
	if a < 'A' || a > 'Z' || b < 'A' || b > 'Z' {
		return ""
	}
	const base = 0x1F1E6
	return string([]rune{rune(base + int(a-'A')), rune(base + int(b-'A'))})
}

// ByAlpha2 looks up a country by ISO-3166-1 alpha-2 code, case-insensitively.
func ByAlpha2(code string) (Country, bool) {
	c, ok := byAlpha2[strings.ToUpper(code)]
	return c, ok
}

// ByAlpha3 looks up a country by ISO-3166-1 alpha-3 code, case-insensitively.
func ByAlpha3(code string) (Country, bool) {
	c, ok := byAlpha3[strings.ToUpper(code)]
	return c, ok
}

// ByNumeric looks up a country by ISO-3166-1 numeric-3 code.
func ByNumeric(code string) (Country, bool) {
	c, ok := byNumeric[code]
	return c, ok
}

// ByDialCode returns every country sharing an E.164 dial code (many share "1").
// The returned slice is shared internal state sorted by Name and must not be
// modified; it is nil when no country uses the code.
func ByDialCode(code string) []Country {
	return byDial[code]
}

// All returns every bundled country sorted by Name. The returned slice is a
// fresh copy the caller may retain and modify.
func All() []Country {
	out := make([]Country, len(sorted))
	copy(out, sorted)
	return out
}
