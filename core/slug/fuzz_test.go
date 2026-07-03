package slug_test

import (
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/slug"
)

func FuzzMake(f *testing.F) {
	seeds := []string{
		"", "Hello World", "Café", "你好世界", "  ---  ", "!!!", "über-cool",
		"Straße", "a b c d e", strings.Repeat("x", 300), "\x00\x01\x02", "😀🎉",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	const sep = "-" // default separator

	f.Fuzz(func(t *testing.T, s string) {
		got := slug.Make(s) // must never panic

		if got == "" {
			return // empty result is always valid
		}
		if strings.HasPrefix(got, sep) || strings.HasSuffix(got, sep) {
			t.Fatalf("Make(%q) = %q has a leading/trailing separator", s, got)
		}
		if strings.Contains(got, sep+sep) {
			t.Fatalf("Make(%q) = %q has a double separator", s, got)
		}
		for _, r := range got {
			isLower := r >= 'a' && r <= 'z'
			isDigit := r >= '0' && r <= '9'
			isSep := string(r) == sep
			if !isLower && !isDigit && !isSep {
				t.Fatalf("Make(%q) = %q contains disallowed rune %q", s, got, r)
			}
		}
	})
}
