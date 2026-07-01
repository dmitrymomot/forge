package sanitize

import (
	"strings"
	"unicode"
)

// Trim removes leading and trailing whitespace (strings.TrimSpace).
func Trim(s string) string {
	return strings.TrimSpace(s)
}

// Collapse trims the string and collapses every internal run of whitespace
// (including tabs and newlines) into a single ASCII space.
func Collapse(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	started := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inSpace = true
			continue
		}
		if inSpace && started {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		started = true
		inSpace = false
	}
	return b.String()
}

// SingleLine removes line breaks (CR, LF, and Unicode line/paragraph
// separators) by treating them as whitespace, then collapses the result to a
// single line with single spaces.
func SingleLine(s string) string {
	// Replacing explicit break runes first keeps Collapse's whitespace handling
	// as the single source of truth for run-collapsing.
	replaced := strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', ' ', ' ', '\v', '\f', '':
			return ' '
		default:
			return r
		}
	}, s)
	return Collapse(replaced)
}

// NoSpaces removes ALL whitespace runes.
func NoSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// StripControl drops Unicode control (Cc) and format (Cf) characters,
// including NUL, while keeping printable runes and normal spaces (space, tab,
// newline pass through as ordinary whitespace).
func StripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return r
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}
