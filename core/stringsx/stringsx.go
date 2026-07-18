package stringsx

import "strings"

// Truncate returns the first n runes of s. n <= 0 returns "".
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Ellipsis returns the first n runes of s, appending "…" when s was longer.
// n <= 0 returns "".
func Ellipsis(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// TruncateWords returns the first n whitespace-separated words of s. Inputs
// with n or fewer words are returned unchanged. n <= 0 returns "".
func TruncateWords(s string, n int) string {
	if n <= 0 {
		return ""
	}
	fields := strings.Fields(s)
	if len(fields) <= n {
		return s
	}
	return strings.Join(fields[:n], " ")
}

// Mask keeps the last keep runes of s and replaces the rest with '*'. When keep
// is greater than or equal to the number of runes in s, or keep <= 0, the whole
// string is masked (never leaks characters).
func Mask(s string, keep int) string {
	r := []rune(s)
	if keep <= 0 || keep >= len(r) {
		return strings.Repeat("*", len(r))
	}
	return strings.Repeat("*", len(r)-keep) + string(r[len(r)-keep:])
}

// Pluralize returns the naive English plural of word for count n. It is a
// best-effort helper for trusted, developer-facing strings only: append "s";
// "es" after s/x/z/ch/sh; consonant + "y" -> "ies". It is NOT a linguistics
// engine (no irregular plurals). Locale-aware pluralization belongs to the
// i18n package. Returns word unchanged when n == 1.
func Pluralize(word string, n int) string {
	if n == 1 || word == "" {
		return word
	}
	lower := strings.ToLower(word)
	switch {
	case strings.HasSuffix(lower, "s"), strings.HasSuffix(lower, "x"),
		strings.HasSuffix(lower, "z"), strings.HasSuffix(lower, "ch"),
		strings.HasSuffix(lower, "sh"):
		return word + "es"
	case strings.HasSuffix(lower, "y") && len(lower) >= 2 && !isVowel(rune(lower[len(lower)-2])):
		return word[:len(word)-1] + "ies"
	default:
		return word + "s"
	}
}

func isVowel(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}
