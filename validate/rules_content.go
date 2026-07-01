package validate

import (
	"regexp"
	"strings"
	"unicode"
)

func allRunes(s string, ok func(rune) bool) bool {
	for _, r := range s {
		if !ok(r) {
			return false
		}
	}
	return true
}

// Alpha requires letters only.
func Alpha(s string) Violation {
	if !allRunes(s, unicode.IsLetter) {
		return Violation{Key: "validation.alpha"}
	}
	return Violation{}
}

// Alphanumeric requires letters and digits only.
func Alphanumeric(s string) Violation {
	if !allRunes(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return Violation{Key: "validation.alphanumeric"}
	}
	return Violation{}
}

// Numeric requires ASCII digits only.
func Numeric(s string) Violation {
	if !allRunes(s, func(r rune) bool { return r >= '0' && r <= '9' }) {
		return Violation{Key: "validation.numeric"}
	}
	return Violation{}
}

// ASCII requires all runes < 128.
func ASCII(s string) Violation {
	if !allRunes(s, func(r rune) bool { return r < 128 }) {
		return Violation{Key: "validation.ascii"}
	}
	return Violation{}
}

// Lowercase requires no uppercase letters.
func Lowercase(s string) Violation {
	if s != strings.ToLower(s) {
		return Violation{Key: "validation.lowercase"}
	}
	return Violation{}
}

// Uppercase requires no lowercase letters.
func Uppercase(s string) Violation {
	if s != strings.ToUpper(s) {
		return Violation{Key: "validation.uppercase"}
	}
	return Violation{}
}

// Contains requires sub to be a substring.
func Contains(sub string) Rule[string] {
	return func(s string) Violation {
		if !strings.Contains(s, sub) {
			return Violation{Key: "validation.contains", Params: []Param{{Key: "sub", Value: sub}}}
		}
		return Violation{}
	}
}

// HasPrefix requires the given prefix.
func HasPrefix(prefix string) Rule[string] {
	return func(s string) Violation {
		if !strings.HasPrefix(s, prefix) {
			return Violation{Key: "validation.has_prefix", Params: []Param{{Key: "prefix", Value: prefix}}}
		}
		return Violation{}
	}
}

// HasSuffix requires the given suffix.
func HasSuffix(suffix string) Rule[string] {
	return func(s string) Violation {
		if !strings.HasSuffix(s, suffix) {
			return Violation{Key: "validation.has_suffix", Params: []Param{{Key: "suffix", Value: suffix}}}
		}
		return Violation{}
	}
}

// Match requires s to match re; the caller names the key.
func Match(re *regexp.Regexp, key string) Rule[string] {
	return func(s string) Violation {
		if !re.MatchString(s) {
			return Violation{Key: key}
		}
		return Violation{}
	}
}
