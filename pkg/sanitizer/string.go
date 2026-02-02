package sanitizer

import (
	"strings"
	"unicode"
)

// Trim removes leading and trailing whitespace from the string.
func Trim(s string) string {
	return strings.TrimSpace(s)
}

// ToLower converts the string to lowercase.
func ToLower(s string) string {
	return strings.ToLower(s)
}

// ToUpper converts the string to uppercase.
func ToUpper(s string) string {
	return strings.ToUpper(s)
}

// ToTitle converts the string to title case.
func ToTitle(s string) string {
	return strings.ToTitle(s)
}

// ToKebabCase prevents consecutive dashes and ensures clean URL-safe identifiers.
func ToKebabCase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))

	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteRune('-')
			prevDash = true
		}
	}

	result := strings.Trim(b.String(), "-")
	return result
}

// ToSnakeCase prevents consecutive underscores for clean database column names.
func ToSnakeCase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))

	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteRune('_')
			prevUnderscore = true
		}
	}

	result := strings.Trim(b.String(), "_")
	return result
}

// ToCamelCase follows JavaScript convention: first word lowercase, subsequent words capitalized.
func ToCamelCase(s string) string {
	s = strings.TrimSpace(s)

	var b strings.Builder
	newWord := false
	first := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if first {
				b.WriteRune(unicode.ToLower(r))
				first = false
				newWord = false
				continue
			}
			if newWord {
				b.WriteRune(unicode.ToUpper(r))
				newWord = false
			} else {
				b.WriteRune(unicode.ToLower(r))
			}
			continue
		}
		if !first {
			newWord = true
		}
	}

	return b.String()
}

// TrimToLower trims whitespace and converts to lowercase in one operation.
func TrimToLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// TrimToUpper trims whitespace and converts to uppercase in one operation.
func TrimToUpper(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// MaxLength handles Unicode properly and prevents buffer overflows from malicious input.
func MaxLength(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	return string(runes[:maxLen])
}

// RemoveExtraWhitespace prevents layout issues and normalizes user input formatting.
func RemoveExtraWhitespace(s string) string {
	normalized := whitespaceRegex.ReplaceAllString(s, " ")
	return strings.TrimSpace(normalized)
}

// RemoveControlChars prevents injection attacks while preserving common whitespace.
func RemoveControlChars(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

// StripHTML removes ALL HTML tags and returns plain text.
// Uses bluemonday StrictPolicy for comprehensive XSS protection.
func StripHTML(s string) string {
	initPolicies()
	return strictPolicy.Sanitize(s)
}

// RemoveChars removes all occurrences of the specified characters from the string.
func RemoveChars(s string, chars string) string {
	for _, char := range chars {
		s = strings.ReplaceAll(s, string(char), "")
	}
	return s
}

// ReplaceChars replaces all occurrences of any character in old with the new string.
func ReplaceChars(s string, old string, new string) string {
	for _, char := range old {
		s = strings.ReplaceAll(s, string(char), new)
	}
	return s
}

// KeepAlphanumeric preserves spaces for readability while removing special characters.
func KeepAlphanumeric(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return -1
	}, s)
}

// KeepAlpha keeps only letters and spaces, removing all other characters.
func KeepAlpha(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsSpace(r) {
			return r
		}
		return -1
	}, s)
}

// KeepDigits keeps only numeric digits, removing all other characters.
func KeepDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

// SingleLine converts multi-line strings to single line by replacing line breaks with spaces.
// Useful for form fields and log messages that need to be on one line.
func SingleLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")

	return RemoveExtraWhitespace(s)
}
