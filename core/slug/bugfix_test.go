package slug_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/slug"
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

// noWholeDanglingSeparator asserts the content-agnostic separator invariants that
// hold for ANY input (including content that legitimately begins or ends with a
// rune that is also a separator prefix): the slug never starts or ends with a WHOLE
// separator and never contains a doubled separator run. Unlike noDanglingSeparator
// it does not check partial separator fragments, because a single leading/trailing
// rune of a multi-rune separator can be legitimate folded content (e.g. sep "oo"
// with input "one", or sep "a-" with input "banana").
func noWholeDanglingSeparator(t *testing.T, got, sep, ctx string) {
	t.Helper()
	if got == "" {
		return
	}
	assert.NotContainsf(t, got, sep+sep, "%s: slug %q contains a doubled separator run", ctx, got)
	assert.Falsef(t, strings.HasPrefix(got, sep), "%s: slug %q starts with separator %q", ctx, got, sep)
	assert.Falsef(t, strings.HasSuffix(got, sep), "%s: slug %q ends with separator %q", ctx, got, sep)
}

// REGRESSION (9b06c52): separator-boundary-aware truncation must never delete
// real folded content, even when the separator's leading rune(s) coincide with a
// prefix of legitimate word content. The prior content-string-matching trim
// greedily stripped such content; these repros must be content-preserving.
func TestMake_MaxLength_SeparatorPrefixContentPreserved(t *testing.T) {
	tests := []struct {
		name string
		in   string
		opts []slug.Option
		want string
	}{
		// separator "a-": "banana" fits in maxLen 20 as a single word, so no
		// inserted separator exists to trim. The trailing "a" is content.
		{"sep-a-dash", "banana", []slug.Option{slug.WithSeparator("a-"), slug.WithMaxLength(20)}, "banana"},
		// separator "oo": "zoo cat" -> word "zoo" alone fits in maxLen 5; the "oo"
		// is content, not an inserted joiner.
		{"sep-oo", "zoo cat", []slug.Option{slug.WithSeparator("oo"), slug.WithMaxLength(5)}, "zoo"},
		// separator "2x": "abc2" folds to a single word "abc2" within maxLen 20;
		// the trailing "2" is content.
		{"sep-2x", "abc2", []slug.Option{slug.WithSeparator("2x"), slug.WithMaxLength(20)}, "abc2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, slug.Make(tt.in, tt.opts...), "Make(%q)", tt.in)
		})
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

// BUG R4: when the separator + random suffix consume the entire maxLength budget,
// baseMax computes to 0. joinWords reads maxLen<=0 as "unlimited", so the FULL base
// was emitted and the result grossly overflowed maxLength. The final result (base +
// sep + suffix) MUST satisfy len([]rune(result)) <= maxLength; the base is dropped
// when there is no room for it.
func TestMake_MaxLength_SuffixConsumesBudget_R4(t *testing.T) {
	tests := []struct {
		name string
		in   string
		opts []slug.Option
		max  int
	}{
		// sep "-" (1) + suffix 4 already exceeds maxLength 3: no room for the base at
		// all. Was "hello-world-foobar-XX" (21 runes).
		{"explicit-suffix", "hello world foobar", []slug.Option{slug.WithMaxLength(3), slug.WithSuffix(4)}, 3},
		// reserved hit forces the default 6-rune suffix; maxLength 3 leaves no room
		// for the "api" base. Was "api-XX" (6 runes).
		{"reserved-suffix", "api", []slug.Option{slug.WithMaxLength(3), slug.WithReservedSlugs("api")}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Random tail: assert the structural invariants over many draws.
			for range 50 {
				got := slug.Make(tt.in, tt.opts...)
				assert.LessOrEqual(t, utf8.RuneCountInString(got), tt.max,
					"Make(%q) = %q must be <= %d runes", tt.in, got, tt.max)
				noDanglingSeparator(t, got, "-")
			}
		})
	}
}

// TestMake_MaxLength_LengthCap_Property is the decisive property test guarding the
// maxLength contract across the full option grid that produced R4. It would have
// caught R4: the length-cap assertion runs over EVERY combination (all separators,
// including ones whose runes appear in content like "oo"/"a-") and fails the moment
// a base overflows its cap — which is exactly the 0==unlimited collision R4 was.
//
// The whole-separator structural invariants (no leading/trailing separator, no
// doubled run) are asserted only for separators whose runes can never be folded
// content ("-", "--", "~~~"). For a separator like "oo" or "a-", a base legitimately
// truncated to a fragment ending in "o"/"a" abuts the joiner and coincides with a
// separator prefix — that content/separator ambiguity is a deliberate non-goal of
// separator-boundary-aware truncation and is out of scope for the length-cap fix.
//
// Iteration is deterministic; the random suffix only affects tail content, never the
// structural invariants, so a single pass per combination is representative.
func TestMake_MaxLength_LengthCap_Property(t *testing.T) {
	inputs := []string{
		"hello world foobar",
		"api",
		"a",
		"aa bb cc dd",
		"administrator",
		"The Quick Brown Fox",
		"你好 world",
		"one",
	}
	// separatorSafe reports whether none of sep's runes can appear in folded content
	// ([a-z0-9]); only for such separators are the whole-separator boundary checks
	// unambiguous (a leading/trailing separator prefix cannot be real content).
	separatorSafe := func(sep string) bool {
		for _, r := range sep {
			if isSlugContentRune(r) {
				return false
			}
		}
		return true
	}
	seps := []string{"-", "--", "~~~", "a-", "oo"}
	maxLengths := []int{1, 2, 3, 4, 5, 8, 12, 50}
	suffixLens := []int{0, 2, 4}
	for _, in := range inputs {
		for _, sep := range seps {
			for _, maxLen := range maxLengths {
				for _, suffixLen := range suffixLens {
					for _, reserved := range []bool{false, true} {
						opts := []slug.Option{
							slug.WithSeparator(sep),
							slug.WithMaxLength(maxLen),
						}
						if suffixLen > 0 {
							opts = append(opts, slug.WithSuffix(suffixLen))
						}
						if reserved {
							// Reserve the plain-folded form so the reserved path is
							// exercised even when it is not otherwise a suffix case.
							opts = append(opts, slug.WithReservedSlugs(slug.Make(in)))
						}
						name := fmt.Sprintf("in=%q/sep=%q/max=%d/suf=%d/res=%v",
							in, sep, maxLen, suffixLen, reserved)
						got := slug.Make(in, opts...)
						// Length cap: asserted over the WHOLE grid. This is the R4 guard.
						assert.LessOrEqualf(t, utf8.RuneCountInString(got), maxLen,
							"%s => %q exceeds maxLength", name, got)
						if separatorSafe(sep) {
							noWholeDanglingSeparator(t, got, sep, name)
						}
					}
				}
			}
		}
	}
}

// isSlugContentRune reports whether r is a rune that folded slug content can contain
// ([a-z0-9]); used to tell separator runes apart from possible content.
func isSlugContentRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
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
