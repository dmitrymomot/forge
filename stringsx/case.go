package stringsx

import (
	"strings"
	"unicode"
)

// splitWords breaks s into words on separators (space, '_', '-') and camelCase
// / acronym boundaries.
func splitWords(s string) []string {
	var words []string
	var cur []rune
	runes := []rune(s)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	for i, r := range runes {
		switch {
		case r == ' ' || r == '_' || r == '-':
			flush()
		case unicode.IsUpper(r):
			prev := rune(0)
			if i > 0 {
				prev = runes[i-1]
			}
			next := rune(0)
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			// Boundary before an uppercase that follows a lower/digit, or that
			// starts a new word after an acronym (UPPERlower).
			if len(cur) > 0 && (unicode.IsLower(prev) || unicode.IsDigit(prev) ||
				(unicode.IsUpper(prev) && unicode.IsLower(next))) {
				flush()
			}
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

func toDelimited(s string, delim string) string {
	words := splitWords(s)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, delim)
}

// ToSnake converts s to snake_case.
func ToSnake(s string) string { return toDelimited(s, "_") }

// ToKebab converts s to kebab-case.
func ToKebab(s string) string { return toDelimited(s, "-") }

// ToCamel converts s to lowerCamelCase mechanically: each word after the first
// is title-cased ("user_id" -> "userId"). It does not special-case acronyms —
// use ToCamelWith to supply them.
func ToCamel(s string) string { return ToCamelWith(s) }

// ToCamelWith is ToCamel with caller-supplied acronyms. Each acronym is matched
// case-insensitively against a word after the first and rendered with the
// acronym's own spelling, so ToCamelWith("user_id", "ID") == "userID" and
// ToCamelWith("get_user_oauth_token", "OAuth") == "getUserOAuthToken". The
// first word is always lowercased (lowerCamelCase), even if it matches an
// acronym. Words not matching any acronym are title-cased.
func ToCamelWith(s string, acronyms ...string) string {
	var rules map[string]string
	if len(acronyms) > 0 {
		rules = make(map[string]string, len(acronyms))
		for _, a := range acronyms {
			rules[strings.ToLower(a)] = a
		}
	}
	words := splitWords(s)
	var b strings.Builder
	for i, w := range words {
		if i == 0 {
			b.WriteString(strings.ToLower(w))
			continue
		}
		lower := strings.ToLower(w)
		if rep, ok := rules[lower]; ok {
			b.WriteString(rep)
			continue
		}
		r := []rune(lower)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}
