package sanitize

import (
	"strings"
	"unicode"
)

// KeepAlpha keeps only Unicode letters and spaces.
func KeepAlpha(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || r == ' ' {
			return r
		}
		return -1
	}, s)
}

// KeepDigits keeps only ASCII digits 0-9.
func KeepDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// KeepAlphanumeric keeps only Unicode letters, Unicode digits, and spaces.
func KeepAlphanumeric(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			return r
		}
		return -1
	}, s)
}

// RemoveChars deletes every rune that appears in chars.
func RemoveChars(s, chars string) string {
	if chars == "" {
		return s
	}
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(chars, r) {
			return -1
		}
		return r
	}, s)
}
