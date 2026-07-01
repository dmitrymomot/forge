package validate

import (
	"strings"
	"unicode/utf8"
)

// Required fails when v is its zero value.
func Required[T comparable](v T) Violation {
	var zero T
	if v == zero {
		return Violation{Key: "validation.required"}
	}
	return Violation{}
}

// NotBlank fails when s is empty or whitespace-only.
func NotBlank(s string) Violation {
	if strings.TrimSpace(s) == "" {
		return Violation{Key: "validation.not_blank"}
	}
	return Violation{}
}

// MinLen requires at least n runes.
func MinLen(n int) Rule[string] {
	return func(s string) Violation {
		if utf8.RuneCountInString(s) < n {
			return Violation{Key: "validation.min_len", Params: []Param{{Key: "min", Value: n}}}
		}
		return Violation{}
	}
}

// MaxLen requires at most n runes.
func MaxLen(n int) Rule[string] {
	return func(s string) Violation {
		if utf8.RuneCountInString(s) > n {
			return Violation{Key: "validation.max_len", Params: []Param{{Key: "max", Value: n}}}
		}
		return Violation{}
	}
}

// LenBetween requires a rune count in [min, max].
func LenBetween(min, max int) Rule[string] {
	return func(s string) Violation {
		n := utf8.RuneCountInString(s)
		if n < min || n > max {
			return Violation{Key: "validation.len_between", Params: []Param{{Key: "min", Value: min}, {Key: "max", Value: max}}}
		}
		return Violation{}
	}
}
