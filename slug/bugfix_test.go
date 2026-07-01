package slug_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/slug"
)

// noDanglingSeparator asserts the structural invariants that must hold for every
// non-empty slug: it never starts or ends with any (partial or whole) separator,
// and never contains a run of more than one separator internally.
func noDanglingSeparator(t *testing.T, got, sep string) {
	t.Helper()
	if got == "" {
		return
	}
	// No internal over-length separator run: two adjacent separators would appear
	// as sep+sep somewhere in the string.
	assert.NotContains(t, got, sep+sep, "slug %q contains a doubled separator run", got)

	// The slug must not end with any non-empty prefix of the separator, which
	// covers both a whole trailing separator and a truncated partial one.
	sepRunes := []rune(sep)
	for n := 1; n <= len(sepRunes); n++ {
		frag := string(sepRunes[:n])
		assert.False(t, strings.HasSuffix(got, frag),
			"slug %q ends with separator fragment %q", got, frag)
		assert.False(t, strings.HasPrefix(got, frag),
			"slug %q starts with separator fragment %q", got, frag)
	}
}

// BUG C7: WithCustomReplace must apply replacements in a deterministic order so
// chained/overlapping keys yield the same output on every run.
func TestMake_CustomReplace_Deterministic(t *testing.T) {
	// Chained keys: "a"->"b" and "b"->"c". Longer-key-wins / stable ordering must
	// pick one outcome, identical across all runs.
	first := slug.Make("a", slug.WithCustomReplace(map[string]string{"a": "b", "b": "c"}))
	for range 50 {
		got := slug.Make("a", slug.WithCustomReplace(map[string]string{"a": "b", "b": "c"}))
		assert.Equal(t, first, got, "custom replace must be deterministic")
	}
}

// BUG C7: longer keys must win over their prefixes regardless of map order.
func TestMake_CustomReplace_LongerKeyWins(t *testing.T) {
	// "ab" (len 2) is applied before "a" (len 1); the input "ab" becomes "x" and
	// the standalone "a" in "a ab" becomes "y". Deterministic expected value.
	got := slug.Make("a ab", slug.WithCustomReplace(map[string]string{
		"a":  "y",
		"ab": "x",
	}))
	for range 50 {
		assert.Equal(t, got, slug.Make("a ab", slug.WithCustomReplace(map[string]string{
			"a":  "y",
			"ab": "x",
		})), "custom replace must be deterministic")
	}
	assert.Equal(t, "y-x", got)
}

// BUG C5: multi-rune separator + WithSuffix + WithMaxLength must never leave a
// partial separator that merges with the joining separator into an over-length run.
func TestMake_MultiRuneSeparator_SuffixTruncation(t *testing.T) {
	re := regexp.MustCompile(`^[a-z0-9]+(--[a-z0-9]+)*$`)
	for range 50 {
		got := slug.Make("aa bb cc",
			slug.WithMaxLength(7),
			slug.WithSuffix(2),
			slug.WithSeparator("--"))
		noDanglingSeparator(t, got, "--")
		assert.Regexp(t, re, got, "slug %q has a malformed separator run", got)
	}
}

// BUG C6: plain max-length truncation with a multi-rune separator must not leave a
// trailing partial separator.
func TestMake_MultiRuneSeparator_MaxLengthTruncation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		sep  string
		opts []slug.Option
	}{
		{"double-dash", "ab cd ef", "--", []slug.Option{slug.WithMaxLength(3), slug.WithSeparator("--")}},
		{"triple-tilde", "a b c", "~~~", []slug.Option{slug.WithMaxLength(2), slug.WithSeparator("~~~")}},
		{"triple-tilde-mid", "aa b c", "~~~", []slug.Option{slug.WithMaxLength(4), slug.WithSeparator("~~~")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slug.Make(tt.in, tt.opts...)
			noDanglingSeparator(t, got, tt.sep)
		})
	}
}
